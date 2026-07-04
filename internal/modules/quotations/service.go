package quotations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrForbidden        = errors.New("forbidden")
	ErrInvalidInput     = errors.New("invalid input")
	ErrAlreadyConverted = errors.New("quotation already converted to an order")
	ErrCustomerRequired = errors.New("customer information required for customer quotations")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListQuotationsRequest) ([]Quotation, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}
	qs, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
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
	if err := s.canView(actor, *q); err != nil {
		return Quotation{}, err
	}
	return *q, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateQuotationRequest) (Quotation, error) {
	// Business rule: admin cannot participate in business transactions
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") || hasRole(actor, "DRIVER") {
		return Quotation{}, ErrForbidden
	}
	// Normalize type
	if input.QuotationType != "INTERNAL" {
		input.QuotationType = "CUSTOMER"
	}
	if err := validateCreate(input); err != nil {
		return Quotation{}, err
	}

	// Resolve nursery info
	var nurseryName, nurseryPhone *string
	if input.NurseryID != nil && *input.NurseryID > 0 {
		// Verify user belongs to nursery (unless admin)
		if !hasRole(actor, "ADMIN") {
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

	// Resolve creator name
	createdByName, _ := s.repository.GetUserName(ctx, actor.UserID)

	q, err := s.repository.Create(ctx, actor.UserID, input, createdByName, nurseryName, nurseryPhone)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", q.ID, actionInsert, input)
	return *q, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, id int64, input UpdateQuotationRequest) (Quotation, error) {
	if hasRole(actor, "DRIVER") {
		return Quotation{}, ErrForbidden
	}
	if len(input.Items) == 0 {
		return Quotation{}, ErrInvalidInput
	}
	for _, item := range input.Items {
		if item.PlantID <= 0 || item.Quantity <= 0 || item.UnitPrice < 0 || item.TotalPrice < 0 {
			return Quotation{}, ErrInvalidInput
		}
		if !validateItemMath(item) {
			return Quotation{}, ErrInvalidInput
		}
	}
	creatorID, err := s.repository.FindCreatorID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	if !hasRole(actor, "ADMIN") && creatorID != actor.UserID {
		return Quotation{}, ErrForbidden
	}
	q, err := s.repository.Update(ctx, id, input)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", id, actionUpdate, input)
	return *q, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, id int64) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		if err := s.repository.SoftDelete(ctx, id); err != nil {
			return err
		}
		s.audit(ctx, actor, "quotations", id, actionDelete, map[string]any{"deleted": true})
		return nil
	}
	// Non-admin: only nursery owner may delete
	ownerID, err := s.repository.FindNurseryOwnerID(ctx, id)
	if err != nil {
		// If no nursery, fall back to creator check
		creatorID, cerr := s.repository.FindCreatorID(ctx, id)
		if cerr != nil {
			return cerr
		}
		if creatorID != actor.UserID {
			return ErrForbidden
		}
	} else if ownerID != actor.UserID {
		return ErrForbidden
	}
	if err := s.repository.SoftDelete(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, actor, "quotations", id, actionDelete, map[string]any{"deleted": true})
	return nil
}

// Approve sends a CUSTOMER_DRAFT quotation to the customer (status → CUSTOMER_SENT).
// Only the nursery owner/manager or admin may do this.
func (s *Service) Approve(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.Status != "CUSTOMER_DRAFT" {
		return Quotation{}, ErrInvalidInput
	}
	approved, err := s.repository.Approve(ctx, quotationID, actor.UserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_SENT"})
	return *approved, nil
}

// ConvertToOrder links a quotation to an existing order and marks it CONVERTED.
func (s *Service) ConvertToOrder(ctx context.Context, actor ActorContext, quotationID int64, orderID int64) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.ConvertedOrderID != nil {
		return Quotation{}, ErrAlreadyConverted
	}
	if err := s.repository.MarkConverted(ctx, quotationID, orderID, actor.UserID); err != nil {
		return Quotation{}, err
	}
	converted, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "CONVERTED", "order_id": orderID})
	return *converted, nil
}

// AssignManager assigns a manager to a quotation. Only the nursery owner or admin may do this.
func (s *Service) AssignManager(ctx context.Context, actor ActorContext, quotationID int64, req AssignManagerRequest) (Quotation, error) {
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		ownerID, err := s.repository.FindNurseryOwnerID(ctx, quotationID)
		if err != nil || ownerID != actor.UserID {
			return Quotation{}, ErrForbidden
		}
	}
	q, err := s.repository.AssignManager(ctx, quotationID, req.ManagerUserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, req)
	return *q, nil
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListQuotationsRequest) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if input.Buying {
		// Buyer perspective: filter by buyer user or buyer nursery
		input.UserID = actor.UserID
		if hasRole(actor, "NURSERY_OWNER") {
			nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
			if nurseryID != nil {
				input.BuyerNurseryID = *nurseryID
			}
		}
		return nil
	}
	// Seller perspective: scoped to nursery if NURSERY_OWNER/MANAGER, else by creator
	if hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER") {
		// NurseryID is set by handler from query param; fallback: auto-scope via getUserNursery
		if input.NurseryID <= 0 {
			nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
			if nurseryID != nil {
				input.NurseryID = *nurseryID
			} else {
				// manager: fall back to user filter
				input.UserID = actor.UserID
			}
		}
		return nil
	}
	// Default: buyer/customer sees their own
	input.UserID = actor.UserID
	return nil
}

// BuyerAccept lets a buyer (or buyer nursery owner) accept a quotation sent to them.
func (s *Service) BuyerAccept(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canBuyerAct(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	updated, err := s.repository.BuyerAccept(ctx, quotationID, actor.UserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "BUYER_ACCEPTED"})
	return *updated, nil
}

// BuyerReject lets a buyer reject a quotation sent to them.
func (s *Service) BuyerReject(ctx context.Context, actor ActorContext, quotationID int64, req AcceptRejectQuotationRequest) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canBuyerAct(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}
	updated, err := s.repository.BuyerReject(ctx, quotationID, actor.UserID, reason)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "BUYER_REJECTED", "reason": reason})
	return *updated, nil
}

// canBuyerAct verifies the actor is the buyer on this quotation.
// Matches both linked accounts (customer_user_id) and unlinked mobile-only quotations.
func (s *Service) canBuyerAct(ctx context.Context, actor ActorContext, q Quotation) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if q.CustomerUserID != nil && *q.CustomerUserID == actor.UserID {
		return nil
	}
	// Allow match by recipient_mobile when customer_user_id is not yet set.
	if q.RecipientMobile != nil && *q.RecipientMobile != "" {
		mobile, err := s.repository.GetUserMobile(ctx, actor.UserID)
		if err == nil && mobile == *q.RecipientMobile {
			return nil
		}
	}
	if q.BuyerNurseryID != nil {
		ownedNurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
		if ownedNurseryID != nil && *ownedNurseryID == *q.BuyerNurseryID {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) canView(actor ActorContext, q Quotation) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if q.CreatedByUserID == actor.UserID {
		return nil
	}
	if q.AssignedManagerUserID != nil && *q.AssignedManagerUserID == actor.UserID {
		return nil
	}
	if q.CustomerUserID != nil && *q.CustomerUserID == actor.UserID {
		return nil
	}
	if q.NurseryID != nil {
		owner, err := s.repository.IsNurseryOwner(context.Background(), *q.NurseryID, actor.UserID)
		if err == nil && owner {
			return nil
		}
		member, err := s.repository.IsNurseryMember(context.Background(), *q.NurseryID, actor.UserID)
		if err != nil {
			return ErrForbidden
		}
		if member {
			return nil
		}
	}
	return ErrForbidden
}

// canManage checks if the actor can approve, convert, or otherwise mutate a quotation.
// Nursery owner and manager (member) can manage; admins always can; creator cannot (they're the buyer).
func (s *Service) canManage(ctx context.Context, actor ActorContext, q Quotation) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if q.NurseryID != nil {
		owner, err := s.repository.IsNurseryOwner(ctx, *q.NurseryID, actor.UserID)
		if err == nil && owner {
			return nil
		}
		member, err := s.repository.IsNurseryMember(ctx, *q.NurseryID, actor.UserID)
		if err == nil && member {
			return nil
		}
	}
	return ErrForbidden
}

func validateCreate(input CreateQuotationRequest) error {
	if len(input.Items) == 0 {
		return ErrInvalidInput
	}
	for _, item := range input.Items {
		if item.PlantID <= 0 || item.Quantity <= 0 || item.UnitPrice < 0 || item.TotalPrice < 0 {
			return ErrInvalidInput
		}
		if !validateItemMath(item) {
			return ErrInvalidInput
		}
	}
	// CUSTOMER quotations must have at least a recipient name or mobile
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

func hasRole(actor ActorContext, role string) bool {
	for _, r := range actor.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func totalPages(total int64, perPage int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}

func (s *Service) audit(ctx context.Context, actor ActorContext, table string, recordID int64, action string, data any) {
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: table,
		RecordID:  recordID,
		Action:    action,
		ChangedBy: actor.UserID,
		SourceIP:  actor.IPAddress,
		UserAgent: actor.UserAgent,
		NewJSON:   mustJSON(data),
		At:        time.Now(),
	})
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
