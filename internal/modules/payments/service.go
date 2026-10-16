package payments

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
)

// SMSSender is the slice of the sms module payments needs for payer alerts.
type SMSSender interface {
	SendToUser(ctx context.Context, userID int64, req sms.SendRequest) (sms.SendResult, error)
}

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	sms        SMSSender
}

func NewService(repository Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, auditSvc: auditSvc}
}

// WithSMS enables payment SMS alerts to the payer.
func (s *Service) WithSMS(sender SMSSender) *Service {
	s.sms = sender
	return s
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListPaymentsRequest) ([]Payment, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}
	payments, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return payments, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, paymentID int64) (Payment, error) {
	payment, err := s.repository.FindByID(ctx, paymentID)
	if err != nil {
		return Payment{}, err
	}
	if err := s.canViewPayment(ctx, actor, *payment); err != nil {
		return Payment{}, err
	}
	return *payment, nil
}

func (s *Service) CreateManual(ctx context.Context, actor ActorContext, input ManualPaymentRequest) (Payment, error) {
	normalized, err := s.normalizeCreateInput(input)
	if err != nil {
		return Payment{}, err
	}
	if err := s.canCreate(ctx, actor, &normalized); err != nil {
		return Payment{}, err
	}
	payment, err := s.repository.Create(ctx, normalized)
	if err != nil {
		return Payment{}, err
	}
	s.audit(ctx, actor, payment.ID, actionInsert, normalized)
	s.notifyPayer(*payment, "")
	return *payment, nil
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, paymentID int64, input UpdateStatusRequest) (Payment, error) {
	status := strings.ToUpper(strings.TrimSpace(input.Status))
	if !isAllowedStatus(status) {
		return Payment{}, ErrInvalidInput
	}
	current, err := s.repository.FindByID(ctx, paymentID)
	if err != nil {
		return Payment{}, err
	}
	if err := s.canManagePayment(ctx, actor, *current); err != nil {
		return Payment{}, err
	}
	previousStatus := current.Status
	updated, err := s.repository.UpdateStatus(ctx, paymentID, UpdatePaymentInput{
		Status:               status,
		TransactionReference: input.TransactionReference,
		Notes:                input.Notes,
		Provider:             input.Provider,
		ProviderPaymentID:    input.ProviderPaymentID,
		ProviderOrderID:      input.ProviderOrderID,
		ProviderSignature:    input.ProviderSignature,
		RawResponseJSON:      jsonOrEmpty(input.RawResponse),
	})
	if err != nil {
		return Payment{}, err
	}
	s.audit(ctx, actor, updated.ID, actionUpdate, input)
	s.notifyPayer(*updated, previousStatus)
	return *updated, nil
}

func (s *Service) normalizeCreateInput(input ManualPaymentRequest) (CreatePaymentInput, error) {
	paymentFor := strings.ToUpper(strings.TrimSpace(input.PaymentFor))
	if paymentFor == "" {
		if input.UserSubscriptionID != nil {
			paymentFor = paymentForSubscription
		} else {
			paymentFor = paymentForOrder
		}
	}
	method := strings.ToUpper(strings.TrimSpace(input.PaymentMethod))
	status := strings.ToUpper(strings.TrimSpace(input.Status))
	if status == "" {
		status = "SUCCESS"
	}
	if input.Amount <= 0 || !isAllowedStatus(status) || !isAllowedPaymentFor(paymentFor) || !isAllowedMethod(method) {
		return CreatePaymentInput{}, ErrInvalidInput
	}
	if paymentFor == paymentForOrder && input.OrderID == nil {
		return CreatePaymentInput{}, ErrInvalidInput
	}
	if paymentFor == paymentForSubscription && input.UserSubscriptionID == nil {
		return CreatePaymentInput{}, ErrInvalidInput
	}
	return CreatePaymentInput{
		PaymentFor:           paymentFor,
		OrderID:              input.OrderID,
		UserSubscriptionID:   input.UserSubscriptionID,
		PayerUserID:          input.PayerUserID,
		Amount:               input.Amount,
		Method:               method,
		TransactionReference: input.TransactionReference,
		Status:               status,
		Notes:                input.Notes,
		Provider:             input.Provider,
		ProviderPaymentID:    input.ProviderPaymentID,
		ProviderOrderID:      input.ProviderOrderID,
		ProviderSignature:    input.ProviderSignature,
		RawResponseJSON:      jsonOrEmpty(input.RawResponse),
	}, nil
}

func (s *Service) canCreate(ctx context.Context, actor ActorContext, input *CreatePaymentInput) error {
	if input.PaymentFor == paymentForOrder {
		access, err := s.repository.OrderAccess(ctx, *input.OrderID)
		if err != nil {
			return err
		}
		if input.PayerUserID == nil && access.BuyerID != nil {
			input.PayerUserID = access.BuyerID
		}
		return s.canAccessOrder(ctx, actor, access)
	}
	access, err := s.repository.SubscriptionAccess(ctx, *input.UserSubscriptionID)
	if err != nil {
		return err
	}
	if input.PayerUserID == nil {
		input.PayerUserID = &access.UserID
	}
	if actor.HasRole("ADMIN") || access.UserID == actor.UserID {
		return nil
	}
	return ErrForbidden
}

func (s *Service) canViewPayment(ctx context.Context, actor ActorContext, payment Payment) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if payment.PayerUserID != nil && *payment.PayerUserID == actor.UserID {
		return nil
	}
	if payment.OrderID != nil {
		access, err := s.repository.OrderAccess(ctx, *payment.OrderID)
		if err != nil {
			return err
		}
		return s.canAccessOrder(ctx, actor, access)
	}
	return ErrForbidden
}

func (s *Service) canManagePayment(ctx context.Context, actor ActorContext, payment Payment) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if payment.PaymentFor == paymentForOrder && payment.OrderID != nil {
		access, err := s.repository.OrderAccess(ctx, *payment.OrderID)
		if err != nil {
			return err
		}
		return s.canAccessOrder(ctx, actor, access)
	}
	if payment.PaymentFor == paymentForSubscription && payment.PayerUserID != nil && *payment.PayerUserID == actor.UserID {
		return nil
	}
	return ErrForbidden
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListPaymentsRequest) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if actor.HasRole("NURSERY_OWNER") {
		if input.OrderID > 0 {
			access, err := s.repository.OrderAccess(ctx, input.OrderID)
			if err != nil {
				return err
			}
			return s.canAccessOrder(ctx, actor, access)
		}
		if input.PayerUserID == 0 {
			input.PayerUserID = actor.UserID
		}
		return nil
	}
	input.PayerUserID = actor.UserID
	return nil
}

func (s *Service) canAccessOrder(ctx context.Context, actor ActorContext, access *OrderAccess) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if access.BuyerID != nil && *access.BuyerID == actor.UserID {
		return nil
	}
	if actor.HasRole("NURSERY_OWNER") && access.NurseryID != nil {
		member, err := s.repository.IsNurseryMember(ctx, *access.NurseryID, actor.UserID)
		if err != nil {
			return err
		}
		if member {
			return nil
		}
	}
	return ErrForbidden
}

func normalizeList(input ListPaymentsRequest) ListPaymentsRequest {
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
	input.PaymentFor = strings.ToUpper(strings.TrimSpace(input.PaymentFor))
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = "desc"
	}
	return input
}

func isAllowedPaymentFor(value string) bool {
	return value == paymentForOrder || value == paymentForSubscription
}

func isAllowedStatus(value string) bool {
	switch value {
	case "PENDING", "SUCCESS", "FAILED", "REFUNDED", "CANCELLED":
		return true
	default:
		return false
	}
}

func isAllowedMethod(value string) bool {
	switch value {
	case "UPI", "CARD", "CASH", "BANK_TRANSFER", "NET_BANKING", "WALLET", "COD", "CHEQUE", "OTHER":
		return true
	default:
		return false
	}
}

func totalPages(total int64, perPage int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}

func jsonOrEmpty(value map[string]any) string {
	if len(value) == 0 {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModulePayments,
		EntityType: "payment",
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}

// paymentSMS maps a payment that just reached SUCCESS or FAILED to its alert
// template; ok is false when no SMS applies.
func paymentSMS(p Payment) (sms.SendRequest, bool) {
	payParts := sms.CodeParts(p.PaymentCode)
	if p.PayerUserID == nil || len(payParts) != 2 {
		return sms.SendRequest{}, false
	}
	rupees, paise := sms.AmountParts(p.Amount)
	values := map[string]string{"amount_rupees": rupees, "amount_paise": paise, "payment_date": payParts[0], "payment_seq": payParts[1]}
	req := sms.SendRequest{Values: values, ReferenceType: "PAYMENT", ReferenceID: &p.ID}
	switch {
	case p.Status == "FAILED":
		req.TemplateCode = sms.TemplatePaymentFailed
	case p.Status == "SUCCESS" && p.PaymentFor == paymentForSubscription:
		req.TemplateCode = sms.TemplatePaymentReceivedSubscription
	case p.Status == "SUCCESS" && p.PaymentFor == paymentForOrder && p.OrderCode != nil:
		orderParts := sms.CodeParts(*p.OrderCode)
		if len(orderParts) != 2 {
			return sms.SendRequest{}, false
		}
		req.TemplateCode = sms.TemplatePaymentReceivedOrder
		values["order_date"], values["order_seq"] = orderParts[0], orderParts[1]
	default:
		return sms.SendRequest{}, false
	}
	return req, true
}

// notifyPayer sends the alert only on a transition into SUCCESS/FAILED, after
// the payment is committed, detached from the request so SMS latency or
// failure never affects the payment itself.
func (s *Service) notifyPayer(p Payment, previousStatus string) {
	if s.sms == nil || p.Status == previousStatus {
		return
	}
	req, ok := paymentSMS(p)
	if !ok {
		return
	}
	payer := *p.PayerUserID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := s.sms.SendToUser(ctx, payer, req); err != nil {
			slog.Warn("payment sms not sent", "payment_id", p.ID, "template_code", req.TemplateCode, "error", err)
		}
	}()
}
