package quotations

import (
	"context"
	"errors"
	"fmt"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/consent"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/phone"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden         = apperrs.ErrForbidden
	ErrInvalidInput      = apperrs.ErrInvalidInput
	ErrAlreadyConverted  = errors.New("quotation already converted to an order")
	ErrCustomerRequired  = errors.New("customer information required for customer quotations")
	ErrInvalidTransition = errors.New("action not allowed in current quotation status")
	ErrQuotationExpired  = errors.New("quotation has expired")
	ErrDocumentNotFound  = errors.New("no official PDF document found for this quotation")
	ErrFileTooLarge      = errors.New("file exceeds maximum allowed size")
	ErrInvalidPDF        = errors.New("file is not a valid PDF")
	ErrRateLimited       = errors.New("too many requests")
)

// IsEditable and BuildCapabilities are defined in policy.go.

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	storage    *storage.Client
	redis      redis.Cmdable
	events     EventNotifier  // SMS; nil = in-app only
	consents   *consent.Store // nil: confirmations checked, not stored (tests)
}

// UseConsents stores the buyer's acknowledgement on accepting.
func (s *Service) UseConsents(c *consent.Store) { s.consents = c }

func NewService(repository Repository, auditSvc *auditlog.Service, storageCli *storage.Client, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: repository, auditSvc: auditSvc, storage: storageCli, redis: rdb}
}

// cachedQuotationList is the Global Search "Quotations" category's
// cache-aside payload, keyed per-actor (see List) -- manager-only redaction
// of customer contact details means the payload itself differs by role, so
// this must never be shared across users.
type cachedQuotationList struct {
	Quotations []Quotation `json:"quotations"`
	Pagination Pagination  `json:"pagination"`
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListQuotationsRequest) ([]Quotation, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}

	if input.Search == "" {
		return s.listQuotations(ctx, actor, input)
	}

	// Global Search's "Quotations" category (short TTL -- status changes
	// frequently). Key built from the fully RBAC-resolved input.
	const module = "quotations"
	redisutil.RecordSearchTerm(ctx, s.redis, nil, module, input.Search)
	gen := redisutil.SearchGeneration(ctx, s.redis, nil, module, actor.UserID)
	key := redisutil.SearchCacheKey(module, actor.UserID, gen,
		input.Search, input.Status, fmt.Sprint(input.NurseryID), fmt.Sprint(input.BuyerNurseryID),
		fmt.Sprint(input.Buying), fmt.Sprint(input.ManagerScopeUserID), fmt.Sprint(input.UnassignedOnly),
		input.SortBy, input.SortOrder, fmt.Sprint(input.Page), fmt.Sprint(input.PerPage))

	if cached, ok := redisutil.GetCachedSearch[cachedQuotationList](ctx, s.redis, nil, module, input.Search, key); ok {
		return cached.Quotations, cached.Pagination, nil
	}

	start := time.Now()
	qs, pagination, err := s.listQuotations(ctx, actor, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	redisutil.SetCachedSearch(ctx, s.redis, nil, module, input.Search, key,
		cachedQuotationList{Quotations: qs, Pagination: pagination}, redisutil.SearchTTLQuotations, time.Since(start))
	return qs, pagination, nil
}

func (s *Service) listQuotations(ctx context.Context, actor ActorContext, input ListQuotationsRequest) ([]Quotation, Pagination, error) {
	qs, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	for i := range qs {
		qs[i] = enrichQuotation(actor, qs[i])
	}
	return qs, Pagination{
		Page:       input.Page,
		PerPage:    input.PerPage,
		Total:      total,
		TotalPages: totalPages(total, input.PerPage),
	}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, id int64) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canView(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	return enrichQuotation(actor, *q), nil
}

func (s *Service) RenderPDF(ctx context.Context, actor ActorContext, id int64) ([]byte, string, error) {
	q, err := s.Get(ctx, actor, id)
	if err != nil {
		return nil, "", err
	}
	// Include verification QR only when an active token exists — read-only, no side effects.
	var verifyURL string
	if v, verErr := s.repository.GetActiveVerificationToken(ctx, id); verErr == nil && v != nil {
		verifyURL = s.verifyURL(v.Token)
	}
	// q is already privacy-masked by s.Get() above, so a nil CustomerUserID here correctly
	// withholds recipient contact details from manager-only actors.
	extras, extrasErr := s.repository.GetPDFContactExtras(ctx, q.NurseryID, q.CustomerUserID)
	if extrasErr != nil {
		extras = PDFContactExtras{}
	}
	pdfBytes := buildQuotationPDF(q, verifyURL, extras)
	s.audit(ctx, actor, auditlog.EntityQuotation, id, auditlog.ActionDownload, map[string]any{
		"masked":    isManagerOnly(actor),
		"generated": true,
	})
	return pdfBytes, q.QuotationCode + ".pdf", nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateQuotationRequest) (Quotation, error) {
	// Business rule: admins and drivers do not participate in business transactions.
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") || actor.HasRole("DRIVER") {
		return Quotation{}, ErrForbidden
	}
	switch input.QuotationType = strings.ToUpper(strings.TrimSpace(input.QuotationType)); input.QuotationType {
	case "":
		input.QuotationType = "CUSTOMER"
	case "CUSTOMER", "INTERNAL":
	default:
		return Quotation{}, ErrInvalidInput
	}
	if err := validateCreate(input); err != nil {
		return Quotation{}, err
	}

	// Normalize recipient_mobile before validation.
	if input.RecipientMobile != nil {
		normalized, ok := phone.Normalize(*input.RecipientMobile)
		if !ok && *input.RecipientMobile != "" {
			return Quotation{}, fmt.Errorf("invalid recipient_mobile: %w", ErrInvalidInput)
		}
		input.RecipientMobile = &normalized
	}
	if err := s.validateCustomerSelection(ctx, Quotation{NurseryID: input.NurseryID}, input.CustomerUserID); err != nil {
		return Quotation{}, err
	}

	var nurseryName, nurseryPhone *string
	if input.NurseryID != nil && *input.NurseryID > 0 {
		if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
			// Nursery must be ACTIVE before quotations can be created.
			active, err := s.repository.IsNurseryActive(ctx, *input.NurseryID)
			if err != nil {
				return Quotation{}, err
			}
			if !active {
				return Quotation{}, ErrForbidden
			}
			member, err := s.repository.IsNurseryMember(ctx, *input.NurseryID, actor.UserID)
			if err != nil {
				return Quotation{}, err
			}
			if !member {
				return Quotation{}, ErrForbidden
			}
		}
		name, phone, err := s.repository.GetNurseryInfo(ctx, *input.NurseryID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Quotation{}, err
		}
		if name != "" {
			nurseryName = &name
		}
		if phone != "" {
			nurseryPhone = &phone
		}
	}

	// Owner-only: validate pre-assigned manager is an active nursery member.
	if input.AssignedManagerUserID != nil && *input.AssignedManagerUserID > 0 {
		if !s.isNurseryOwner(ctx, actor, Quotation{NurseryID: input.NurseryID}) {
			return Quotation{}, ErrForbidden
		} else if input.NurseryID != nil {
			member, err := s.repository.IsNurseryMember(ctx, *input.NurseryID, *input.AssignedManagerUserID)
			if err != nil || !member {
				return Quotation{}, ErrInvalidInput
			}
		}
	}

	createdByName, _ := s.repository.GetUserName(ctx, actor.UserID)
	q, err := s.repository.Create(ctx, actor.UserID, input, createdByName, nurseryName, nurseryPhone)
	if err != nil {
		return Quotation{}, err
	}
	s.scheduleQuotationExpiry(ctx, q)
	// Assigned while creating: the manager is told, as with Assign Manager.
	if input.AssignedManagerUserID != nil && *input.AssignedManagerUserID > 0 {
		s.notifyAssigned(ctx, *input.AssignedManagerUserID, q)
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, q.ID, actionInsert, input)
	s.bumpQuotationsSearchCache(ctx, actor.UserID)
	return enrichQuotation(actor, *q), nil
}

// notifyAssigned tells a manager a quotation is now theirs to handle.
func (s *Service) notifyAssigned(ctx context.Context, managerID int64, q *Quotation) {
	s.enqueueNotification(ctx, managerID,
		"QUOTATION_ASSIGNED",
		"Quotation Assigned to You",
		fmt.Sprintf("You have been assigned to quotation %s.", q.QuotationCode),
		notifyqueue.Link("quotation", q.ID))
}

// bumpQuotationsSearchCache invalidates the acting user's previously-cached
// Global Search "Quotations" results (see List's cache-aside path). See
// searchcache.go's package doc for why other affected actors instead rely
// on the short TTL rather than a cross-actor invalidation fan-out.
func (s *Service) bumpQuotationsSearchCache(ctx context.Context, userID int64) {
	redisutil.BumpSearchGeneration(ctx, s.redis, nil, "quotations", userID)
}

func (s *Service) scheduleQuotationExpiry(ctx context.Context, q *Quotation) {
	if s.redis == nil || q == nil || q.ValidUntil == nil {
		return
	}
	if err := s.redis.ZAdd(ctx, redisutil.KeyQuotationExpiry, redis.Z{
		Score:  float64(q.ValidUntil.Unix()),
		Member: strconv.FormatInt(q.ID, 10),
	}).Err(); err != nil {
		slog.Warn("redis quotation expiry schedule failed", "quotation_id", q.ID, "error", err)
	}
}

func (s *Service) Update(ctx context.Context, actor ActorContext, id int64, input UpdateQuotationRequest) (Quotation, error) {
	if actor.HasRole("DRIVER") {
		return Quotation{}, ErrForbidden
	}
	if len(input.Items) == 0 || isExpiredTime(input.ValidUntil) {
		return Quotation{}, ErrInvalidInput
	}
	if input.RecipientMobile != nil {
		normalized, ok := phone.Normalize(*input.RecipientMobile)
		if !ok && *input.RecipientMobile != "" {
			return Quotation{}, fmt.Errorf("invalid recipient_mobile: %w", ErrInvalidInput)
		}
		input.RecipientMobile = &normalized
	}
	for i := range input.Items {
		if err := normalizeItem(&input.Items[i], true); err != nil {
			return Quotation{}, err
		}
	}
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	// Business rule: quotations are editable only until approved (i.e., while in a DRAFT status).
	if !IsEditable(q.Status) {
		return Quotation{}, ErrInvalidTransition
	}
	// Business rule: both nursery owners and managers can edit quotations.
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	// Exclusive-editor rule: once a quotation is assigned to a manager, only that
	// manager may edit its content.  The owner regains edit access only after
	// reassigning the quotation to themselves or removing the assignment.
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		if q.AssignedManagerUserID != nil && *q.AssignedManagerUserID != actor.UserID {
			return Quotation{}, ErrForbidden
		}
	}
	// Managers may edit quote content, but customer identity/contact remains
	// owner-controlled and is often redacted from manager clients.
	if !s.isNurseryOwner(ctx, actor, *q) && !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		input.CustomerUserID = q.CustomerUserID
		input.RecipientName = q.RecipientName
		input.RecipientMobile = q.RecipientMobile
	}
	if err := s.validateCustomerSelection(ctx, *q, input.CustomerUserID); err != nil {
		return Quotation{}, err
	}
	updated, err := s.repository.Update(ctx, id, actor.UserID, input)
	if err != nil {
		return Quotation{}, err
	}
	s.scheduleQuotationExpiry(ctx, updated)
	s.audit(ctx, actor, auditlog.EntityQuotation, id, actionUpdate, input)
	s.bumpQuotationsSearchCache(ctx, actor.UserID)
	return enrichQuotation(actor, *updated), nil
}

func (s *Service) UpdateCustomer(ctx context.Context, actor ActorContext, id int64, input UpdateQuotationCustomerRequest) (Quotation, error) {
	if input.RecipientMobile != nil {
		normalized, ok := phone.Normalize(*input.RecipientMobile)
		if !ok && *input.RecipientMobile != "" {
			return Quotation{}, fmt.Errorf("invalid recipient_mobile: %w", ErrInvalidInput)
		}
		input.RecipientMobile = &normalized
	}
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	if q.QuotationType == "INTERNAL" {
		return Quotation{}, ErrInvalidInput
	}
	if q.ConvertedOrderID != nil || q.CustomerRespondedAt != nil {
		return Quotation{}, ErrInvalidTransition
	}
	if !s.isNurseryOwner(ctx, actor, *q) {
		return Quotation{}, ErrForbidden
	}
	if err := s.validateCustomerSelection(ctx, *q, input.CustomerUserID); err != nil {
		return Quotation{}, err
	}
	if input.CustomerUserID == nil && optionalStringValue(input.RecipientName) == "" && optionalStringValue(input.RecipientMobile) == "" {
		return Quotation{}, ErrCustomerRequired
	}
	updated, err := s.repository.UpdateCustomer(ctx, id, input)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, id, actionUpdate, map[string]any{"customer": input})
	return enrichQuotation(actor, *updated), nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, id int64) error {
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return err
	}
	// Converted quotations are permanently locked — no role may delete them.
	if q.ConvertedOrderID != nil {
		return ErrAlreadyConverted
	}
	if !IsDeletable(*q) {
		return ErrInvalidTransition
	}
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		if err := s.repository.SoftDelete(ctx, id); err != nil {
			return err
		}
		s.audit(ctx, actor, auditlog.EntityQuotation, id, actionDelete, map[string]any{"deleted": true})
		return nil
	}
	// Business rule: only the nursery owner may delete a quotation; managers cannot.
	if !s.isNurseryOwner(ctx, actor, *q) {
		return ErrForbidden
	}
	if err := s.repository.SoftDelete(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, id, actionDelete, map[string]any{"deleted": true})
	return nil
}

// SendToCustomer makes a CUSTOMER_DRAFT quotation visible to the buyer
// (status -> CUSTOMER_SENT). Only nursery owner, manager, or admin may do this.
func (s *Service) SendToCustomer(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "quotations", quotationID)
	if err != nil {
		return Quotation{}, err
	}
	defer lock.Release(ctx)

	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	switch q.Status {
	case "CUSTOMER_DRAFT":
	case "CUSTOMER_REQUESTED":
		if !requestOpen(*q) {
			return Quotation{}, ErrRequestExpired
		}
		if !allPriced(q.Items) {
			return Quotation{}, ErrRequestNotPriced
		}
	default:
		return Quotation{}, ErrInvalidTransition
	}
	if isExpiredTime(q.ValidUntil) {
		return Quotation{}, ErrQuotationExpired
	}
	approved, err := s.repository.Approve(ctx, quotationID, actor.UserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_SENT"})
	s.scheduleQuotationExpiry(ctx, approved)
	// Notify the buyer if their account is linked.
	if approved.CustomerUserID != nil {
		nurseryName := ""
		if approved.NurseryName != nil {
			nurseryName = *approved.NurseryName
		}
		s.enqueueNotification(ctx, *approved.CustomerUserID,
			notifyqueue.EventQuotationSent,
			"Quotation Ready for Review",
			fmt.Sprintf("Quotation %s from %s is ready for your review.", approved.QuotationCode, nurseryName), notifyqueue.Link("quotation", approved.ID))
	}
	// Not on the app (never signed in, or only a mobile number): by SMS.
	if s.events != nil && (approved.CustomerUserID != nil || approved.RecipientMobile != nil) {
		ev := messaging.Event{Code: sms.TemplateQuotationReceived, NotOnAppOnly: true,
			Values:        map[string]string{"quotation_code": approved.QuotationCode, "amount": messaging.FormatAmount(approved.TotalAmount)},
			ReferenceType: "QUOTATION", ReferenceID: &approved.ID}
		if approved.CustomerUserID != nil {
			ev.UserID = *approved.CustomerUserID
		} else {
			ev.Mobile = *approved.RecipientMobile
		}
		s.events.NotifyAsync(ev)
	}
	return enrichQuotation(actor, *approved), nil
}

// Approve is kept as a backward-compatible alias for older clients.
func (s *Service) Approve(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	return s.SendToCustomer(ctx, actor, quotationID)
}

// Recall pulls a CUSTOMER_SENT quotation back to CUSTOMER_DRAFT so it can be edited before re-sending.
func (s *Service) Recall(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "quotations", quotationID)
	if err != nil {
		return Quotation{}, err
	}
	defer lock.Release(ctx)

	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.Status != "CUSTOMER_SENT" {
		return Quotation{}, ErrInvalidTransition
	}
	recalled, err := s.repository.Recall(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_DRAFT", "recalled": true})
	return enrichQuotation(actor, *recalled), nil
}

// ConvertToOrder auto-creates a PENDING order from the quotation's items and marks it CONVERTED.
func (s *Service) ConvertToOrder(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "quotations", quotationID)
	if err != nil {
		return Quotation{}, err
	}
	defer lock.Release(ctx)

	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.Status != "CUSTOMER_ACCEPTED" {
		return Quotation{}, ErrInvalidTransition
	}
	if q.ConvertedOrderID != nil {
		return Quotation{}, ErrAlreadyConverted
	}
	orderID, err := s.repository.CreateOrderAndConvert(ctx, q, actor.UserID)
	if err != nil {
		return Quotation{}, err
	}
	converted, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, actionUpdate, map[string]any{"status": "CONVERTED", "order_id": orderID})
	s.bumpQuotationsSearchCache(ctx, actor.UserID)
	// A new order was just created by this conversion -- invalidate this
	// actor's cached "Orders" search results too, not just "Quotations".
	redisutil.BumpSearchGeneration(ctx, s.redis, nil, "orders", actor.UserID)
	return enrichQuotation(actor, *converted), nil
}

// AssignManager assigns an active nursery member as the responsible manager for a quotation.
// Only the nursery owner or admin may do this; the target must be an active member of the nursery.
func (s *Service) AssignManager(ctx context.Context, actor ActorContext, quotationID int64, req AssignManagerRequest) (Quotation, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "quotations", quotationID)
	if err != nil {
		return Quotation{}, err
	}
	defer lock.Release(ctx)

	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if q.ConvertedOrderID != nil {
		return Quotation{}, ErrAlreadyConverted
	}
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		if !s.isNurseryOwner(ctx, actor, *q) {
			return Quotation{}, ErrForbidden
		}
	}
	// Target user must be an active member of the quotation's nursery.
	if q.NurseryID != nil {
		member, err := s.repository.IsNurseryMember(ctx, *q.NurseryID, req.ManagerUserID)
		if err != nil || !member {
			return Quotation{}, ErrInvalidInput
		}
	}
	updated, err := s.repository.AssignManager(ctx, quotationID, req.ManagerUserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, actionUpdate, req)
	s.notifyAssigned(ctx, req.ManagerUserID, updated)
	return enrichQuotation(actor, *updated), nil
}

// UnassignManager removes the assigned manager from a quotation, making it owner-private again.
// Only the nursery owner or admin may do this.
func (s *Service) UnassignManager(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if q.ConvertedOrderID != nil {
		return Quotation{}, ErrAlreadyConverted
	}
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		if !s.isNurseryOwner(ctx, actor, *q) {
			return Quotation{}, ErrForbidden
		}
	}
	updated, err := s.repository.UnassignManager(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, actionUpdate, map[string]any{"assigned_manager_user_id": nil})
	return enrichQuotation(actor, *updated), nil
}

// BuyerAccept lets the buyer accept a quotation that has been sent to them.
// given is the buyer's acknowledgement (BUYER_QUOTATION_ACCEPT): plants,
// quantities, prices and that payment is between them and the nursery.
func (s *Service) BuyerAccept(ctx context.Context, actor ActorContext, quotationID int64, given *consent.Given) (Quotation, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "quotations", quotationID)
	if err != nil {
		return Quotation{}, err
	}
	defer lock.Release(ctx)

	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canBuyerAct(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.Status != "CUSTOMER_SENT" {
		return Quotation{}, ErrInvalidTransition
	}
	// Asked only of someone who may accept, of a quotation that can be accepted.
	if err := consent.Check(consent.BuyerQuotationAccept, given); err != nil {
		return Quotation{}, err
	}
	updated, err := s.repository.BuyerAccept(ctx, quotationID, actor.UserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_ACCEPTED"})
	if s.consents != nil {
		if err := s.consents.Save(ctx, consent.Record{Type: consent.BuyerQuotationAccept, UserID: actor.UserID, UserRole: "BUYER",
			QuotationID: &quotationID, Evidence: given.Evidence, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}); err != nil {
			slog.Error("consent not stored", "step", consent.BuyerQuotationAccept, "quotation_id", quotationID, "error", err)
		}
	}
	if ownerID, err := s.repository.FindNurseryOwnerID(ctx, quotationID); err == nil {
		s.enqueueNotification(ctx, ownerID,
			notifyqueue.EventQuotationAccepted,
			"Quotation Accepted",
			acceptedMessage(updated.QuotationCode, s.deliveryKM(ctx, quotationID, actor.UserID)),
			notifyqueue.Link("quotation", quotationID))
	}
	return enrichQuotation(actor, *updated), nil
}

// BuyerReject lets the buyer reject a quotation that has been sent to them.
func (s *Service) BuyerReject(ctx context.Context, actor ActorContext, quotationID int64, req AcceptRejectQuotationRequest) (Quotation, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "quotations", quotationID)
	if err != nil {
		return Quotation{}, err
	}
	defer lock.Release(ctx)

	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canBuyerAct(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.Status != "CUSTOMER_SENT" {
		return Quotation{}, ErrInvalidTransition
	}
	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}
	updated, err := s.repository.BuyerReject(ctx, quotationID, actor.UserID, reason)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_REJECTED", "reason": reason})
	if ownerID, err := s.repository.FindNurseryOwnerID(ctx, quotationID); err == nil {
		msg := fmt.Sprintf("Buyer rejected quotation %s.", updated.QuotationCode)
		if reason != "" {
			msg += " Reason: " + reason
		}
		s.enqueueNotification(ctx, ownerID,
			"QUOTATION_REJECTED",
			"Quotation Rejected",
			msg, notifyqueue.Link("quotation", quotationID))
	}
	return enrichQuotation(actor, *updated), nil
}

// EventNotifier raises SMS/email events (messaging.Notifier).
type EventNotifier interface {
	NotifyAsync(ev messaging.Event)
}

// SetEvents turns on SMS for quotation events (nil = in-app only).
func (s *Service) SetEvents(n EventNotifier) { s.events = n }

func (s *Service) enqueueNotification(ctx context.Context, userID int64, notifType, title, message string, data map[string]any) {
	err := notifyqueue.Enqueue(ctx, s.redis, notifyqueue.Event{
		UserID:  userID,
		Type:    notifType,
		Title:   title,
		Message: message,
		Data:    data,
	})
	if err == nil {
		return
	}
	slog.Warn("notification queue enqueue failed; falling back to direct notification", "type", notifType, "user_id", userID, "error", err)
	_ = s.repository.CreateNotification(ctx, userID, notifType, title, message, data)
}

// RecordDownload records a quotation PDF generation/download event in the audit log.
// It verifies the actor may view the quotation before writing the record.
func (s *Service) RecordDownload(ctx context.Context, actor ActorContext, quotationID int64, masked bool) error {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return err
	}
	if err := s.canView(ctx, actor, *q); err != nil {
		return err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, auditlog.ActionDownload, map[string]any{
		"masked": masked,
	})
	return nil
}

// enrichQuotation shapes every quotation the API returns, so manager-only
// actors never receive customer details from any endpoint.
func enrichQuotation(actor ActorContext, q Quotation) Quotation {
	// Notes are the nursery's internal notes; who handles it is internal too.
	if !sellerView(actor, q) {
		q.Notes = nil
		q.AssignedManagerUserID = nil
		q.AssignedManagerName = nil
	}
	if isManagerOnly(actor) {
		redactCustomerContact(&q)
	}
	status := strings.ToUpper(strings.TrimSpace(q.Status))
	q.Status = status
	q.Lifecycle = lifecyclePtr(lifecycle.Quotation(status))
	q.ExpirySummary = quotationExpirySummary(q.ValidUntil)
	q.Capabilities = BuildCapabilities(actor, q)
	return q
}

func quotationExpirySummary(validUntil *time.Time) *QuotationExpirySummary {
	if validUntil == nil {
		return nil
	}
	now := time.Now()
	days := int(validUntil.Sub(now).Hours() / 24)
	if validUntil.After(now) && days == 0 {
		days = 1
	}
	return &QuotationExpirySummary{
		IsExpired:     validUntil.Before(now),
		DaysRemaining: &days,
	}
}

func lifecyclePtr[T any](value T) *T {
	return &value
}

// ── validation ────────────────────────────────────────────────────────────────

func validateCreate(input CreateQuotationRequest) error {
	if len(input.Items) == 0 || isExpiredTime(input.ValidUntil) {
		return ErrInvalidInput
	}
	for i := range input.Items {
		if err := normalizeItem(&input.Items[i], true); err != nil {
			return err
		}
	}
	// CUSTOMER quotations require at least one recipient identifier.
	if input.QuotationType == "CUSTOMER" {
		hasRecipient := (input.RecipientName != nil && *input.RecipientName != "") ||
			(input.RecipientMobile != nil && *input.RecipientMobile != "") ||
			input.CustomerUserID != nil ||
			input.BuyerNurseryID != nil
		if !hasRecipient {
			return ErrCustomerRequired
		}
	}
	return nil
}

func normalizeList(input ListQuotationsRequest) ListQuotationsRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Search = strings.TrimSpace(input.Search)
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	return input
}

func totalPages(total int64, perPage int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, newValue any) {
	if s.auditSvc == nil {
		return
	}
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleQuotations,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}

func (s *Service) validateCustomerSelection(ctx context.Context, q Quotation, customerUserID *int64) error {
	if customerUserID == nil {
		return nil
	}
	if q.NurseryID == nil || *q.NurseryID <= 0 || *customerUserID <= 0 {
		return ErrInvalidInput
	}
	linked, err := s.repository.IsNurseryCustomer(ctx, *q.NurseryID, *customerUserID)
	if err != nil {
		return err
	}
	if !linked {
		return ErrForbidden
	}
	return nil
}

func optionalStringValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func (s *Service) deliveryKM(ctx context.Context, quotationID, buyerUserID int64) *float64 {
	km, err := s.repository.DeliveryKM(ctx, quotationID, buyerUserID)
	if err != nil {
		return nil // only a hint in a notification: never fail the accept
	}
	return km
}

// acceptedMessage tells the nursery roughly how far the delivery is; the
// buyer was shown the same before accepting. Transport is never in the price.
func acceptedMessage(code string, km *float64) string {
	msg := fmt.Sprintf("Buyer accepted quotation %s.", code)
	if km != nil {
		msg += fmt.Sprintf(" Delivery is about %.0f km away (approximate).", *km)
	}
	return msg + " Transport charges are not included; agree them with the buyer."
}
