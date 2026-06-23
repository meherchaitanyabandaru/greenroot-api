package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("conflict")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListPlans(ctx context.Context) ([]SubscriptionPlan, error) {
	return s.repository.ListPlans(ctx, true)
}

func (s *Service) GetPlan(ctx context.Context, planID int64) (SubscriptionPlan, error) {
	plan, err := s.repository.FindPlan(ctx, planID)
	if err != nil {
		return SubscriptionPlan{}, err
	}
	return *plan, nil
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListSubscriptionsRequest) ([]UserSubscription, Pagination, error) {
	input = normalizeList(actor, input)
	if !hasRole(actor, "ADMIN") && input.UserID != actor.UserID {
		return nil, Pagination{}, ErrForbidden
	}
	subscriptions, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return subscriptions, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, subscriptionID int64) (UserSubscription, error) {
	subscription, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	if err := s.canAccess(actor, *subscription); err != nil {
		return UserSubscription{}, err
	}
	return *subscription, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateSubscriptionRequest) (UserSubscription, error) {
	userID := actor.UserID
	if input.UserID != nil {
		if !hasRole(actor, "ADMIN") && *input.UserID != actor.UserID {
			return UserSubscription{}, ErrForbidden
		}
		userID = *input.UserID
	}
	if userID <= 0 || input.PlanID <= 0 {
		return UserSubscription{}, ErrInvalidInput
	}
	if _, err := s.repository.FindActiveByUser(ctx, userID); err == nil {
		return UserSubscription{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return UserSubscription{}, err
	}
	plan, err := s.repository.FindPlan(ctx, input.PlanID)
	if err != nil {
		return UserSubscription{}, err
	}
	if !plan.IsActive {
		return UserSubscription{}, ErrInvalidInput
	}
	startDate, err := parseStartDate(input.StartDate)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	endDate, amount, err := cycleEndAndAmount(startDate, strings.ToUpper(strings.TrimSpace(input.BillingCycle)), *plan)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	subscription, err := s.repository.Create(ctx, CreateSubscriptionInput{
		UserID:    userID,
		PlanID:    input.PlanID,
		StartDate: startDate,
		EndDate:   endDate,
		AutoRenew: input.AutoRenew,
	})
	if err != nil {
		return UserSubscription{}, err
	}
	if err := s.createSubscriptionPayment(ctx, subscription.ID, userID, amount, input.PaymentMethod, input.Provider, input.ProviderRef, "Subscription created"); err != nil {
		return UserSubscription{}, err
	}
	s.audit(ctx, actor, subscription.ID, actionInsert, map[string]any{"plan_id": input.PlanID, "user_id": userID, "billing_cycle": input.BillingCycle})
	return *subscription, nil
}

func (s *Service) Me(ctx context.Context, actor ActorContext) ([]UserSubscription, Pagination, error) {
	return s.List(ctx, actor, ListSubscriptionsRequest{UserID: actor.UserID, Page: 1, PerPage: 50})
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, subscriptionID int64, input UpdateStatusRequest) (UserSubscription, error) {
	if !hasRole(actor, "ADMIN") {
		return UserSubscription{}, ErrForbidden
	}
	status := strings.ToUpper(strings.TrimSpace(input.Status))
	if !isAllowedStatus(status) {
		return UserSubscription{}, ErrInvalidInput
	}
	subscription, err := s.repository.UpdateStatus(ctx, subscriptionID, status)
	if err != nil {
		return UserSubscription{}, err
	}
	s.audit(ctx, actor, subscription.ID, actionUpdate, map[string]any{"subscription_status": status})
	return *subscription, nil
}

func (s *Service) Renew(ctx context.Context, actor ActorContext, subscriptionID int64, input RenewSubscriptionRequest) (UserSubscription, error) {
	current, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	if err := s.canAccess(actor, *current); err != nil {
		return UserSubscription{}, err
	}
	plan, err := s.repository.FindPlan(ctx, current.PlanID)
	if err != nil {
		return UserSubscription{}, err
	}
	startDate := time.Now().Truncate(24 * time.Hour)
	if current.EndDate != nil && current.EndDate.After(startDate) {
		startDate = current.EndDate.AddDate(0, 0, 1)
	}
	endDate, amount, err := cycleEndAndAmount(startDate, strings.ToUpper(strings.TrimSpace(input.BillingCycle)), *plan)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	subscription, err := s.repository.Renew(ctx, subscriptionID, RenewSubscriptionInput{
		StartDate: startDate,
		EndDate:   endDate,
		AutoRenew: current.AutoRenew,
	})
	if err != nil {
		return UserSubscription{}, err
	}
	if err := s.createSubscriptionPayment(ctx, subscription.ID, current.UserID, amount, input.PaymentMethod, input.Provider, input.ProviderRef, "Subscription renewed"); err != nil {
		return UserSubscription{}, err
	}
	s.audit(ctx, actor, subscription.ID, actionUpdate, map[string]any{"renewed_until": endDate.Format(time.DateOnly), "billing_cycle": input.BillingCycle})
	return *subscription, nil
}

func (s *Service) Cancel(ctx context.Context, actor ActorContext, subscriptionID int64, input CancelSubscriptionRequest) (UserSubscription, error) {
	current, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	if err := s.canAccess(actor, *current); err != nil {
		return UserSubscription{}, err
	}
	subscription, err := s.repository.UpdateStatus(ctx, subscriptionID, statusCancelled)
	if err != nil {
		return UserSubscription{}, err
	}
	s.audit(ctx, actor, subscription.ID, actionUpdate, map[string]any{"subscription_status": statusCancelled, "reason": stringOrEmpty(input.Reason), "cancel_immediately": input.CancelImmediately})
	return *subscription, nil
}

func (s *Service) canAccess(actor ActorContext, subscription UserSubscription) error {
	if hasRole(actor, "ADMIN") || subscription.UserID == actor.UserID {
		return nil
	}
	return ErrForbidden
}

func (s *Service) createSubscriptionPayment(ctx context.Context, subscriptionID int64, userID int64, amount float64, method *string, provider *string, providerRef *string, notes string) error {
	if amount <= 0 {
		return nil
	}
	normalizedMethod := strings.ToUpper(strings.TrimSpace(stringOrEmpty(method)))
	if normalizedMethod == "" {
		normalizedMethod = "CARD"
	}
	if !isAllowedPaymentMethod(normalizedMethod) {
		return ErrInvalidInput
	}
	status := "PENDING"
	if strings.TrimSpace(stringOrEmpty(provider)) == "" && normalizedMethod != "CARD" {
		status = "SUCCESS"
	}
	return s.repository.CreatePayment(ctx, CreatePaymentInput{
		SubscriptionID:  subscriptionID,
		PayerUserID:     userID,
		Amount:          amount,
		Method:          normalizedMethod,
		Status:          status,
		Provider:        provider,
		ProviderOrderID: providerRef,
		Notes:           notes,
	})
}

func (s *Service) audit(ctx context.Context, actor ActorContext, recordID int64, action string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: "user_subscriptions",
		RecordID:  recordID,
		Action:    action,
		ChangedBy: actor.UserID,
		SourceIP:  actor.IPAddress,
		UserAgent: actor.UserAgent,
		NewJSON:   string(data),
		At:        time.Now(),
	})
}

func normalizeList(actor ActorContext, input ListSubscriptionsRequest) ListSubscriptionsRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if !hasRole(actor, "ADMIN") {
		input.UserID = actor.UserID
	}
	return input
}

func parseStartDate(value *string) (time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return time.Now().Truncate(24 * time.Hour), nil
	}
	return time.Parse(time.DateOnly, strings.TrimSpace(*value))
}

func cycleEndAndAmount(startDate time.Time, cycle string, plan SubscriptionPlan) (time.Time, float64, error) {
	switch cycle {
	case "", "MONTHLY":
		amount := float64OrZero(plan.MonthlyPrice)
		return startDate.AddDate(0, 1, -1), amount, nil
	case "YEARLY", "ANNUAL":
		amount := float64OrZero(plan.YearlyPrice)
		return startDate.AddDate(1, 0, -1), amount, nil
	default:
		return time.Time{}, 0, ErrInvalidInput
	}
}

func isAllowedStatus(value string) bool {
	switch value {
	case statusActive, statusPaused, statusCancelled, statusExpired:
		return true
	default:
		return false
	}
}

func isAllowedPaymentMethod(value string) bool {
	switch value {
	case "UPI", "CARD", "CASH", "BANK_TRANSFER", "NET_BANKING", "WALLET", "COD", "CHEQUE", "OTHER":
		return true
	default:
		return false
	}
}

func hasRole(actor ActorContext, role string) bool {
	for _, current := range actor.Roles {
		if strings.EqualFold(current, role) {
			return true
		}
	}
	return false
}

func totalPages(total int64, perPage int) int {
	if perPage <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(perPage)))
}

func float64OrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
