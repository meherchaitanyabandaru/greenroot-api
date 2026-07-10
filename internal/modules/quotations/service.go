package quotations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
)

var (
	ErrForbidden         = errors.New("forbidden")
	ErrInvalidInput      = errors.New("invalid input")
	ErrAlreadyConverted  = errors.New("quotation already converted to an order")
	ErrCustomerRequired  = errors.New("customer information required for customer quotations")
	ErrInvalidTransition = errors.New("action not allowed in current quotation status")
	ErrQuotationExpired  = errors.New("quotation has expired")
)

var nonDigit = regexp.MustCompile(`\D`)

// editableStatuses enumerates statuses in which a quotation's content may be modified.
var editableStatuses = map[string]bool{
	"INTERNAL_DRAFT": true,
	"CUSTOMER_DRAFT": true,
}

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
}

func NewService(repository Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, auditSvc: auditSvc}
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
	if isManagerOnly(actor) {
		for i := range qs {
			redactCustomerContact(&qs[i])
		}
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
	if isManagerOnly(actor) {
		redactCustomerContact(q)
	}
	return *q, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateQuotationRequest) (Quotation, error) {
	// Business rule: admins and drivers do not participate in business transactions.
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") || hasRole(actor, "DRIVER") {
		return Quotation{}, ErrForbidden
	}
	if input.QuotationType != "INTERNAL" {
		input.QuotationType = "CUSTOMER"
	}
	if err := validateCreate(input); err != nil {
		return Quotation{}, err
	}

	// Normalize recipient_mobile before validation.
	if input.RecipientMobile != nil {
		normalized := normalizeIndianMobile(*input.RecipientMobile)
		if normalized == "" && *input.RecipientMobile != "" {
			return Quotation{}, fmt.Errorf("invalid recipient_mobile: %w", ErrInvalidInput)
		}
		input.RecipientMobile = &normalized
	}

	var nurseryName, nurseryPhone *string
	if input.NurseryID != nil && *input.NurseryID > 0 {
		if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
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
			input.AssignedManagerUserID = nil // non-owners cannot pre-assign
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
	if input.RecipientMobile != nil {
		normalized := normalizeIndianMobile(*input.RecipientMobile)
		if normalized == "" && *input.RecipientMobile != "" {
			return Quotation{}, fmt.Errorf("invalid recipient_mobile: %w", ErrInvalidInput)
		}
		input.RecipientMobile = &normalized
	}
	for _, item := range input.Items {
		if item.PlantID <= 0 || item.Quantity <= 0 || item.UnitPrice < 0 || item.TotalPrice < 0 {
			return Quotation{}, ErrInvalidInput
		}
		if !validateItemMath(item) {
			return Quotation{}, ErrInvalidInput
		}
	}
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	// Business rule: quotations are editable only until approved (i.e., while in a DRAFT status).
	if !editableStatuses[q.Status] {
		return Quotation{}, ErrInvalidTransition
	}
	// Business rule: both nursery owners and managers can edit quotations.
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	// Exclusive-editor rule: once a quotation is assigned to a manager, only that
	// manager may edit its content.  The owner regains edit access only after
	// reassigning the quotation to themselves or removing the assignment.
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		if q.AssignedManagerUserID != nil && *q.AssignedManagerUserID != actor.UserID {
			return Quotation{}, ErrForbidden
		}
	}
	updated, err := s.repository.Update(ctx, id, input)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", id, actionUpdate, input)
	return *updated, nil
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
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		if err := s.repository.SoftDelete(ctx, id); err != nil {
			return err
		}
		s.audit(ctx, actor, "quotations", id, actionDelete, map[string]any{"deleted": true})
		return nil
	}
	// Business rule: only the nursery owner may delete a quotation; managers cannot.
	if !s.isNurseryOwner(ctx, actor, *q) {
		return ErrForbidden
	}
	if err := s.repository.SoftDelete(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, actor, "quotations", id, actionDelete, map[string]any{"deleted": true})
	return nil
}

// SendToCustomer makes a CUSTOMER_DRAFT quotation visible to the buyer
// (status -> CUSTOMER_SENT). Only nursery owner, manager, or admin may do this.
func (s *Service) SendToCustomer(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.Status != "CUSTOMER_DRAFT" {
		return Quotation{}, ErrInvalidTransition
	}
	approved, err := s.repository.Approve(ctx, quotationID, actor.UserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_SENT"})
	// Notify the buyer if their account is linked.
	if approved.CustomerUserID != nil {
		nurseryName := ""
		if approved.NurseryName != nil {
			nurseryName = *approved.NurseryName
		}
		_ = s.repository.CreateNotification(ctx, *approved.CustomerUserID,
			"QUOTATION_SENT",
			"Quotation Ready for Review",
			fmt.Sprintf("Quotation %s from %s is ready for your review.", approved.QuotationCode, nurseryName))
	}
	return *approved, nil
}

// Approve is kept as a backward-compatible alias for older clients.
func (s *Service) Approve(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
	return s.SendToCustomer(ctx, actor, quotationID)
}

// Recall pulls a CUSTOMER_SENT quotation back to CUSTOMER_DRAFT so it can be edited before re-sending.
func (s *Service) Recall(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
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
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_DRAFT", "recalled": true})
	return *recalled, nil
}

// ConvertToOrder auto-creates a PENDING order from the quotation's items and marks it CONVERTED.
func (s *Service) ConvertToOrder(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
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
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "CONVERTED", "order_id": orderID})
	return *converted, nil
}

// AssignManager assigns an active nursery member as the responsible manager for a quotation.
// Only the nursery owner or admin may do this; the target must be an active member of the nursery.
func (s *Service) AssignManager(ctx context.Context, actor ActorContext, quotationID int64, req AssignManagerRequest) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if q.ConvertedOrderID != nil {
		return Quotation{}, ErrAlreadyConverted
	}
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
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
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, req)
	_ = s.repository.CreateNotification(ctx, req.ManagerUserID,
		"QUOTATION_ASSIGNED",
		"Quotation Assigned to You",
		fmt.Sprintf("You have been assigned to quotation %s.", updated.QuotationCode))
	return *updated, nil
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
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		if !s.isNurseryOwner(ctx, actor, *q) {
			return Quotation{}, ErrForbidden
		}
	}
	updated, err := s.repository.UnassignManager(ctx, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"assigned_manager_user_id": nil})
	return *updated, nil
}

// BuyerAccept lets the buyer accept a quotation that has been sent to them.
func (s *Service) BuyerAccept(ctx context.Context, actor ActorContext, quotationID int64) (Quotation, error) {
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
	updated, err := s.repository.BuyerAccept(ctx, quotationID, actor.UserID)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_ACCEPTED"})
	if ownerID, err := s.repository.FindNurseryOwnerID(ctx, quotationID); err == nil {
		_ = s.repository.CreateNotification(ctx, ownerID,
			"QUOTATION_ACCEPTED",
			"Quotation Accepted",
			fmt.Sprintf("Buyer accepted quotation %s.", updated.QuotationCode))
	}
	return *updated, nil
}

// BuyerReject lets the buyer reject a quotation that has been sent to them.
func (s *Service) BuyerReject(ctx context.Context, actor ActorContext, quotationID int64, req AcceptRejectQuotationRequest) (Quotation, error) {
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
	s.audit(ctx, actor, "quotations", quotationID, actionUpdate, map[string]any{"status": "CUSTOMER_REJECTED", "reason": reason})
	if ownerID, err := s.repository.FindNurseryOwnerID(ctx, quotationID); err == nil {
		msg := fmt.Sprintf("Buyer rejected quotation %s.", updated.QuotationCode)
		if reason != "" {
			msg += " Reason: " + reason
		}
		_ = s.repository.CreateNotification(ctx, ownerID,
			"QUOTATION_REJECTED",
			"Quotation Rejected",
			msg)
	}
	return *updated, nil
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

// canBuyerAct verifies the actor is the buyer on this quotation.
// Matches both linked accounts (customer_user_id) and unlinked mobile-only quotations.
// Also enforces valid_until expiry.
func (s *Service) canBuyerAct(ctx context.Context, actor ActorContext, q Quotation) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	// Expiry check: buyer cannot act on an expired quotation.
	if q.ValidUntil != nil && time.Now().After(*q.ValidUntil) {
		return ErrQuotationExpired
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

// canView checks whether the actor may read a quotation.
// Uses the passed context so request cancellation/timeouts are respected.
func (s *Service) canView(ctx context.Context, actor ActorContext, q Quotation) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if q.CreatedByUserID == actor.UserID {
		return nil
	}
	if q.AssignedManagerUserID != nil && *q.AssignedManagerUserID == actor.UserID {
		return nil
	}
	if q.QuotationType == "CUSTOMER" && q.Status == "CUSTOMER_DRAFT" {
		return ErrForbidden
	}
	if q.CustomerUserID != nil && *q.CustomerUserID == actor.UserID {
		return nil
	}
	// Allow buyers matched by mobile (quotations where customer_user_id is not yet set
	// but recipient_mobile matches — consistent with the buyer list scope).
	if q.RecipientMobile != nil && *q.RecipientMobile != "" {
		mobile, err := s.repository.GetUserMobile(ctx, actor.UserID)
		if err == nil && mobile == *q.RecipientMobile {
			return nil
		}
	}
	// Allow access when the actor's nursery is the designated buyer nursery.
	if q.BuyerNurseryID != nil {
		ownedNurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
		if ownedNurseryID != nil && *ownedNurseryID == *q.BuyerNurseryID {
			return nil
		}
	}
	if q.NurseryID != nil {
		owner, err := s.repository.IsNurseryOwner(ctx, *q.NurseryID, actor.UserID)
		if err == nil && owner {
			return nil
		}
		// Manager-only actors: already checked creator and assignee above.
		// Don't grant access to any other nursery member — they must be explicitly
		// assigned by the owner to see a quotation they didn't create themselves.
		if isManagerOnly(actor) {
			return ErrForbidden
		}
		member, err := s.repository.IsNurseryMember(ctx, *q.NurseryID, actor.UserID)
		if err != nil {
			return ErrForbidden
		}
		if member {
			return nil
		}
	}
	return ErrForbidden
}

// canManage checks if the actor may approve, recall, convert, or otherwise mutate a quotation's state.
// Both nursery owners and managers qualify; admins always qualify.
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

// isNurseryOwner returns true if actor owns the nursery associated with the quotation.
func (s *Service) isNurseryOwner(ctx context.Context, actor ActorContext, q Quotation) bool {
	if q.NurseryID == nil {
		return false
	}
	owner, err := s.repository.IsNurseryOwner(ctx, *q.NurseryID, actor.UserID)
	return err == nil && owner
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListQuotationsRequest) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if input.Buying {
		// Buyer perspective: filter by buyer user or buyer nursery.
		input.UserID = actor.UserID
		if hasRole(actor, "NURSERY_OWNER") {
			nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
			if nurseryID != nil {
				input.BuyerNurseryID = *nurseryID
			}
		}
		return nil
	}
	// Seller perspective: always force scope to the actor's own nursery.
	// Never trust a client-supplied nursery_id — an owner could read another nursery by passing ?nursery_id=X.
	if hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER") {
		nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
		if nurseryID == nil && hasRole(actor, "MANAGER") {
			nurseryID, _ = s.repository.GetManagerNurseryID(ctx, actor.UserID)
		}
		input.UserID = 0
		if nurseryID != nil {
			input.NurseryID = *nurseryID
		} else {
			input.NurseryID = 0
			input.UserID = actor.UserID
		}
		// Manager-only: restrict to quotations they created or are assigned to.
		// Owner (or owner+manager dual-role) always sees the full nursery list.
		if isManagerOnly(actor) {
			input.ManagerScopeUserID = actor.UserID
			input.UnassignedOnly = false // managers never see the "unassigned" owner view
		}
		return nil
	}
	// Default: buyer/customer sees only their own.
	input.UserID = actor.UserID
	return nil
}

// ── validation ────────────────────────────────────────────────────────────────

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

// isManagerOnly returns true when the actor is a manager but NOT also an owner or admin.
func isManagerOnly(actor ActorContext) bool {
	return hasRole(actor, "MANAGER") &&
		!hasRole(actor, "NURSERY_OWNER") &&
		!hasRole(actor, "ADMIN") &&
		!hasRole(actor, "SUPER_ADMIN")
}

// redactCustomerContact removes recipient contact details for actors who must not see them.
func redactCustomerContact(q *Quotation) {
	q.RecipientName = nil
	q.RecipientMobile = nil
}

// normalizeIndianMobile strips non-digits and country prefix, returning a clean
// 10-digit mobile number. Returns "" if the result is not exactly 10 digits.
func normalizeIndianMobile(s string) string {
	s = strings.TrimSpace(s)
	// Strip leading +91 or 91
	s = strings.TrimPrefix(s, "+91")
	if len(s) == 12 && strings.HasPrefix(s, "91") {
		s = s[2:]
	}
	s = nonDigit.ReplaceAllString(s, "")
	if len(s) != 10 {
		return ""
	}
	return s
}
