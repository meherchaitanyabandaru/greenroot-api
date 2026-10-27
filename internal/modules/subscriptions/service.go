package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
	ErrConflict     = errors.New("conflict")
	ErrPaused       = errors.New("subscription paused by admin")
	// ErrPaidAhead: the subscription already runs past MaxPaidAhead; renewing
	// again would only stack more time (e.g. a renew button pressed 3 times).
	ErrPaidAhead = errors.New("subscription already paid far ahead")
)

// MaxPaidAhead: renewals are refused while the current period ends later than this.
const MaxPaidAhead = 365 * 24 * time.Hour

func paidTooFarAhead(current UserSubscription) bool {
	return current.EndDate != nil && time.Until(*current.EndDate) > MaxPaidAhead
}

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	redis      redis.Cmdable
	gateway    PaymentGateway // nil = mock payments
	// webhookSecret verifies Razorpay webhooks; "" = webhook refused.
	webhookSecret string
}

func NewService(repository Repository, auditSvc *auditlog.Service, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: repository, auditSvc: auditSvc, redis: rdb}
}

func (s *Service) ListPlans(ctx context.Context) ([]SubscriptionPlan, error) {
	if plans, ok := s.getCachedPlans(ctx); ok {
		return plans, nil
	}
	plans, err := s.repository.ListPlans(ctx, true)
	if err != nil {
		return nil, err
	}
	s.cachePlans(ctx, plans)
	return plans, nil
}

func (s *Service) getCachedPlans(ctx context.Context) ([]SubscriptionPlan, bool) {
	if s.redis == nil {
		return nil, false
	}
	data, err := s.redis.Get(ctx, redisutil.KeySubscriptionPlans).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false
	}
	if err != nil {
		slog.Warn("redis subscription plan cache read failed", "error", err)
		return nil, false
	}
	var plans []SubscriptionPlan
	if err := json.Unmarshal(data, &plans); err != nil {
		slog.Warn("redis subscription plan cache decode failed", "error", err)
		return nil, false
	}
	return plans, true
}

func (s *Service) cachePlans(ctx context.Context, plans []SubscriptionPlan) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(plans)
	if err != nil {
		slog.Warn("redis subscription plan cache encode failed", "error", err)
		return
	}
	if err := s.redis.Set(ctx, redisutil.KeySubscriptionPlans, data, time.Hour).Err(); err != nil {
		slog.Warn("redis subscription plan cache write failed", "error", err)
	}
}

func (s *Service) invalidatePlanCache(ctx context.Context) {
	if s.redis == nil {
		return
	}
	if err := s.redis.Del(ctx, redisutil.KeySubscriptionPlans).Err(); err != nil {
		slog.Warn("redis subscription plan cache invalidation failed", "error", err)
	}
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
	if !actor.HasRole("ADMIN") && input.UserID != actor.UserID {
		return nil, Pagination{}, ErrForbidden
	}
	subscriptions, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	for i := range subscriptions {
		subscriptions[i] = enrichSubscription(actor, subscriptions[i])
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
	return enrichSubscription(actor, *subscription), nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateSubscriptionRequest) (UserSubscription, error) {
	userID := actor.UserID
	if input.UserID != nil {
		if !actor.HasRole("ADMIN") && *input.UserID != actor.UserID {
			return UserSubscription{}, ErrForbidden
		}
		userID = *input.UserID
	}
	if userID <= 0 || input.PlanID <= 0 {
		return UserSubscription{}, ErrInvalidInput
	}
	// One new subscription at a time per user (the app and the webhook may race).
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "subscription-create", userID)
	if err != nil {
		return UserSubscription{}, err
	}
	defer lock.Release(ctx)
	// A retry of a payment that already created the subscription: same answer.
	if done, err := s.appliedSubscription(ctx, userID, "new", input.Provider, input.ProviderRef, input.ProviderPaymentID); err != nil || done != nil {
		if err != nil {
			return UserSubscription{}, err
		}
		return enrichSubscription(actor, *done), nil
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
	if !plan.IsActive || (isFreePlan(plan.Code) && !actor.HasRole("ADMIN")) {
		return UserSubscription{}, ErrInvalidInput
	}
	startDate, err := parseStartDate(input.StartDate)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	cycle, ok := canonicalCycle(input.BillingCycle)
	if !ok {
		return UserSubscription{}, ErrInvalidInput
	}
	endDate, amount, err := cycleEndAndAmount(startDate, cycle, *plan)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	finalAmount, promoID, err := s.applyPromoDiscount(ctx, input.PromoCode, plan.Code, cycle, amount)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	gst, charged := withGST(finalAmount)
	paid, err := s.checkPayment(ctx, actor, userID, "new", input.PlanID, cycle, charged, input.Provider, input.ProviderRef, input.ProviderPaymentID, input.ProviderSignature, input.trusted)
	if err != nil {
		return UserSubscription{}, err
	}
	// The subscription and its payment record: both or neither.
	var subscription *UserSubscription
	err = s.repository.InTx(ctx, func(repo Repository) error {
		created, err := repo.Create(ctx, CreateSubscriptionInput{
			UserID:    userID,
			PlanID:    input.PlanID,
			StartDate: startDate,
			EndDate:   endDate,
			AutoRenew: input.AutoRenew,
		})
		if err != nil {
			return err
		}
		subscription = created
		return s.createSubscriptionPayment(ctx, repo, created.ID, userID, charged, gst, input.PaymentMethod, input.Provider, input.ProviderRef, paid, "Subscription created")
	})
	if err != nil {
		return UserSubscription{}, err
	}
	if promoID > 0 {
		_ = s.repository.IncrementPromoUsed(ctx, promoID)
	}
	s.scheduleSubscriptionExpiry(ctx, subscription)
	s.claimsChanged(ctx, userID)
	s.audit(ctx, actor, subscription.ID, actionInsert, map[string]any{"plan_id": input.PlanID, "user_id": userID, "billing_cycle": input.BillingCycle, "promo_code": input.PromoCode})
	return enrichSubscription(actor, *subscription), nil
}

// claimsChanged makes the owner's (and their managers') access tokens refresh
// so the subscription gate sees this change now, not after the token expires.
func (s *Service) claimsChanged(ctx context.Context, ownerUserID int64) {
	if s.redis == nil {
		return
	}
	nurseryID, err := s.repository.OwnedNurseryID(ctx, ownerUserID)
	if err != nil {
		slog.Warn("owned nursery lookup failed", "user_id", ownerUserID, "error", err)
		return
	}
	// Longer than any access token, so every pre-change token is caught.
	redisutil.MarkNurseryClaimsStale(ctx, s.redis, slog.Default(), nurseryID, time.Hour)
	// Workspaces carry the plan's state for managers: drop their cached copies.
	people := []int64{ownerUserID}
	if ids, err := s.repository.NurseryManagerIDs(ctx, nurseryID); err == nil {
		people = append(people, ids...)
	}
	redisutil.InvalidateWorkspaces(ctx, s.redis, slog.Default(), people...)
}

func (s *Service) Me(ctx context.Context, actor ActorContext) ([]UserSubscription, Pagination, error) {
	// Managers are nursery employees and do not hold subscriptions.
	if actor.HasRole("MANAGER") {
		return nil, Pagination{}, ErrForbidden
	}
	return s.List(ctx, actor, ListSubscriptionsRequest{UserID: actor.UserID, Page: 1, PerPage: 50})
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, subscriptionID int64, input UpdateStatusRequest) (UserSubscription, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "subscriptions", subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	defer lock.Release(ctx)

	if !actor.HasRole("ADMIN") {
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
	s.claimsChanged(ctx, subscription.UserID)
	s.audit(ctx, actor, subscription.ID, actionUpdate, map[string]any{"subscription_status": status})
	return enrichSubscription(actor, *subscription), nil
}

func (s *Service) Renew(ctx context.Context, actor ActorContext, subscriptionID int64, input RenewSubscriptionRequest) (UserSubscription, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "subscriptions", subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	defer lock.Release(ctx)

	current, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	if err := s.canAccess(actor, *current); err != nil {
		return UserSubscription{}, err
	}
	// A retry of a payment that already renewed this subscription: same answer
	// (before the paid-ahead check, which that renewal may now trip).
	if done, err := s.appliedSubscription(ctx, current.UserID, strconv.FormatInt(subscriptionID, 10), input.Provider, input.ProviderRef, input.ProviderPaymentID); err != nil || done != nil {
		if err != nil {
			return UserSubscription{}, err
		}
		return enrichSubscription(actor, *done), nil
	}
	// A pause is an admin decision; paying must not silently lift it.
	if current.Status == statusPaused && !actor.HasRole("ADMIN") {
		return UserSubscription{}, ErrPaused
	}
	if paidTooFarAhead(*current) {
		return UserSubscription{}, ErrPaidAhead
	}
	planID := current.PlanID
	if input.PlanID != nil && *input.PlanID > 0 {
		planID = *input.PlanID
	}
	plan, err := s.repository.FindPlan(ctx, planID)
	if err != nil {
		return UserSubscription{}, err
	}
	// Free plans come only from nursery approval (or an admin extending one):
	// an owner renewing must move to a paid plan, or the trial never ends.
	if !plan.IsActive || (isFreePlan(plan.Code) && !actor.HasRole("ADMIN")) {
		return UserSubscription{}, ErrInvalidInput
	}
	startDate := businessDay(time.Now())
	if current.EndDate != nil && current.EndDate.After(startDate) {
		startDate = current.EndDate.AddDate(0, 0, 1)
	}
	cycle, ok := canonicalCycle(input.BillingCycle)
	if !ok {
		return UserSubscription{}, ErrInvalidInput
	}
	endDate, amount, err := cycleEndAndAmount(startDate, cycle, *plan)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	finalAmount, promoID, err := s.applyPromoDiscount(ctx, input.PromoCode, plan.Code, cycle, amount)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	gst, charged := withGST(finalAmount)
	paid, err := s.checkPayment(ctx, actor, current.UserID, strconv.FormatInt(subscriptionID, 10), planID, cycle, charged, input.Provider, input.ProviderRef, input.ProviderPaymentID, input.ProviderSignature, input.trusted)
	if err != nil {
		return UserSubscription{}, err
	}
	// The new period and its payment record: both or neither.
	var subscription *UserSubscription
	err = s.repository.InTx(ctx, func(repo Repository) error {
		renewed, err := repo.Renew(ctx, subscriptionID, RenewSubscriptionInput{
			PlanID:    planID,
			StartDate: startDate,
			EndDate:   endDate,
			AutoRenew: current.AutoRenew,
		})
		if err != nil {
			return err
		}
		subscription = renewed
		return s.createSubscriptionPayment(ctx, repo, renewed.ID, current.UserID, charged, gst, input.PaymentMethod, input.Provider, input.ProviderRef, paid, "Subscription renewed")
	})
	if err != nil {
		return UserSubscription{}, err
	}
	if promoID > 0 {
		_ = s.repository.IncrementPromoUsed(ctx, promoID)
	}
	s.scheduleSubscriptionExpiry(ctx, subscription)
	s.claimsChanged(ctx, current.UserID)
	s.audit(ctx, actor, subscription.ID, actionUpdate, map[string]any{"renewed_until": endDate.Format(time.DateOnly), "billing_cycle": input.BillingCycle, "promo_code": input.PromoCode})
	return enrichSubscription(actor, *subscription), nil
}

func (s *Service) scheduleSubscriptionExpiry(ctx context.Context, subscription *UserSubscription) {
	if s.redis == nil || subscription == nil || subscription.EndDate == nil {
		return
	}
	if err := s.redis.ZAdd(ctx, redisutil.KeySubscriptionExpiry, redis.Z{
		Score:  float64(subscription.EndDate.Unix()),
		Member: strconv.FormatInt(subscription.ID, 10),
	}).Err(); err != nil {
		slog.Warn("redis subscription expiry schedule failed", "subscription_id", subscription.ID, "error", err)
	}
}

func (s *Service) Cancel(ctx context.Context, actor ActorContext, subscriptionID int64, input CancelSubscriptionRequest) (UserSubscription, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "subscriptions", subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	defer lock.Release(ctx)

	current, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	if err := s.canAccess(actor, *current); err != nil {
		return UserSubscription{}, err
	}
	if IsTerminal(current.Status) {
		return UserSubscription{}, ErrConflict // already cancelled or expired
	}
	// By default the owner keeps what they paid for: the subscription runs
	// to end_date and then becomes CANCELLED. cancel_immediately ends it now
	// (no refund; refunds are an admin action on the payment).
	paidAhead := current.EndDate != nil && !current.EndDate.Before(businessDay(time.Now()))
	if !input.CancelImmediately && paidAhead && (current.Status == statusActive || current.Status == statusTrial) {
		subscription, err := s.repository.RequestCancel(ctx, subscriptionID)
		if err != nil {
			return UserSubscription{}, err
		}
		s.audit(ctx, actor, subscription.ID, actionUpdate, map[string]any{
			"cancel_requested": true, "ends_on": subscription.EndDate, "reason": stringOrEmpty(input.Reason)})
		return enrichSubscription(actor, *subscription), nil
	}
	subscription, err := s.repository.UpdateStatus(ctx, subscriptionID, statusCancelled)
	if err != nil {
		return UserSubscription{}, err
	}
	s.claimsChanged(ctx, current.UserID)
	s.audit(ctx, actor, subscription.ID, actionUpdate, map[string]any{"subscription_status": statusCancelled, "reason": stringOrEmpty(input.Reason), "cancel_immediately": input.CancelImmediately})
	return enrichSubscription(actor, *subscription), nil
}

func (s *Service) ListPayments(ctx context.Context, actor ActorContext, subscriptionID int64) ([]Payment, error) {
	subscription, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if err := s.canAccess(actor, *subscription); err != nil {
		return nil, err
	}
	return s.repository.ListPaymentsBySubscription(ctx, subscriptionID)
}

// CreateTrialForOwner auto-creates the free TRIAL subscription (TrialMonths) when a nursery is approved.
// Silently succeeds if the owner already has an active subscription or if TRIAL plan is not seeded.
func (s *Service) CreateTrialForOwner(ctx context.Context, ownerUserID int64, approvalDate time.Time) error {
	if _, err := s.repository.FindActiveByUser(ctx, ownerUserID); err == nil {
		return nil // already has an active subscription
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	plan, err := s.repository.FindPlanByCode(ctx, "TRIAL")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // TRIAL plan not seeded yet; skip gracefully
		}
		return err
	}
	startDateStr := businessDay(approvalDate).Format(time.DateOnly)
	adminActor := ActorContext{UserID: ownerUserID, Roles: []string{"ADMIN"}}
	_, err = s.Create(ctx, adminActor, CreateSubscriptionRequest{
		UserID:       &ownerUserID,
		PlanID:       plan.ID,
		BillingCycle: "TRIAL",
		StartDate:    &startDateStr,
	})
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

func enrichSubscription(actor ActorContext, subscription UserSubscription) UserSubscription {
	status := strings.ToUpper(strings.TrimSpace(subscription.Status))
	subscription.Status = status

	daysRemaining := subscription.DaysRemaining
	if daysRemaining == nil && subscription.EndDate != nil {
		days := calendarDaysLeft(*subscription.EndDate, time.Now())
		daysRemaining = &days
		subscription.DaysRemaining = daysRemaining
	}

	isActive := status == statusActive
	isExpired := status == statusExpired || (daysRemaining != nil && *daysRemaining < 0)
	isExpiringSoon := isActive && daysRemaining != nil && *daysRemaining >= 0 && *daysRemaining <= 14
	paymentStatus := ""
	if subscription.LatestPayment != nil {
		paymentStatus = strings.ToUpper(strings.TrimSpace(subscription.LatestPayment.Status))
	}

	subscription.Lifecycle = lifecyclePtr(lifecycle.Subscription(status, daysRemaining))
	subscription.Summary = &SubscriptionSummary{
		IsActive:       isActive,
		IsExpired:      isExpired,
		IsExpiringSoon: isExpiringSoon,
		PaymentStatus:  paymentStatus,
	}
	caps := BuildCapabilities(actor, subscription)
	subscription.Capabilities = &caps
	return subscription
}

func lifecyclePtr[T any](value T) *T {
	return &value
}

func (s *Service) canAccess(actor ActorContext, subscription UserSubscription) error {
	if actor.HasRole("ADMIN") || subscription.UserID == actor.UserID {
		return nil
	}
	return ErrForbidden
}

func (s *Service) createSubscriptionPayment(ctx context.Context, repo Repository, subscriptionID int64, userID int64, amount, gst float64, method *string, provider *string, providerRef *string, paid *verifiedPayment, notes string) error {
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
	input := CreatePaymentInput{
		SubscriptionID:  subscriptionID,
		PayerUserID:     userID,
		Amount:          amount,
		GSTAmount:       gst,
		Method:          normalizedMethod,
		Status:          status,
		Provider:        provider,
		ProviderOrderID: providerRef,
		Notes:           notes,
	}
	if paid != nil {
		gateway := providerRazorpay
		input.Status, input.Provider = "SUCCESS", &gateway
		if paid.Method != "" {
			input.Method = paid.Method // what was actually used in checkout
		}
		input.ProviderOrderID, input.ProviderPaymentID, input.ProviderSignature = &paid.OrderID, &paid.PaymentID, &paid.Signature
	}
	return repo.CreatePayment(ctx, input)
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleSubscriptions,
		EntityType: "user_subscription",
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
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
	if !actor.HasRole("ADMIN") {
		input.UserID = actor.UserID
	}
	return input
}

func parseStartDate(value *string) (time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return businessDay(time.Now()), nil
	}
	return time.Parse(time.DateOnly, strings.TrimSpace(*value))
}

// cycleEndAndAmount: the period a cycle buys from startDate and its price.
// cycle must be canonical (canonicalCycle).
func cycleEndAndAmount(startDate time.Time, cycle string, plan SubscriptionPlan) (time.Time, float64, error) {
	if cycle == "TRIAL" {
		return startDate.AddDate(0, TrialMonths, -1), 0, nil
	}
	t, ok := termFor(cycle)
	if !ok {
		return time.Time{}, 0, ErrInvalidInput
	}
	return startDate.AddDate(0, t.Months, -1), cyclePrice(float64OrZero(plan.MonthlyPrice), t), nil
}

// ── Promo service methods ─────────────────────────────────────────────────────

func (s *Service) ListPromos(ctx context.Context, actor ActorContext) ([]SubscriptionPromo, error) {
	if !actor.HasRole("ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.ListPromos(ctx, false)
}

func (s *Service) CreatePromo(ctx context.Context, actor ActorContext, req CreatePromoRequest) (SubscriptionPromo, error) {
	if !actor.HasRole("ADMIN") {
		return SubscriptionPromo{}, ErrForbidden
	}
	if err := validatePromoRequest(req.PromoCode, req.DiscountType, req.DiscountValue, req.ValidFrom, req.ValidUntil); err != nil {
		return SubscriptionPromo{}, err
	}
	promo, err := s.repository.CreatePromo(ctx, CreatePromoInput{
		PromoCode:        req.PromoCode,
		Name:             req.Name,
		Description:      req.Description,
		DiscountType:     strings.ToUpper(req.DiscountType),
		DiscountValue:    req.DiscountValue,
		MaxDiscountCap:   req.MaxDiscountCap,
		ApplicablePlans:  req.ApplicablePlans,
		ApplicableCycles: req.ApplicableCycles,
		ValidFrom:        req.ValidFrom,
		ValidUntil:       req.ValidUntil,
		MaxUses:          req.MaxUses,
		CreatedBy:        actor.UserID,
	})
	if err != nil {
		return SubscriptionPromo{}, err
	}
	s.audit(ctx, actor, promo.ID, actionInsert, req)
	return *promo, nil
}

func (s *Service) UpdatePromo(ctx context.Context, actor ActorContext, promoID int64, req UpdatePromoRequest) (SubscriptionPromo, error) {
	if !actor.HasRole("ADMIN") {
		return SubscriptionPromo{}, ErrForbidden
	}
	if err := validatePromoRequest("CODE", req.DiscountType, req.DiscountValue, req.ValidFrom, req.ValidUntil); err != nil {
		return SubscriptionPromo{}, err
	}
	promo, err := s.repository.UpdatePromo(ctx, promoID, UpdatePromoInput{
		Name:             req.Name,
		Description:      req.Description,
		DiscountType:     strings.ToUpper(req.DiscountType),
		DiscountValue:    req.DiscountValue,
		MaxDiscountCap:   req.MaxDiscountCap,
		ApplicablePlans:  req.ApplicablePlans,
		ApplicableCycles: req.ApplicableCycles,
		ValidFrom:        req.ValidFrom,
		ValidUntil:       req.ValidUntil,
		IsActive:         req.IsActive,
		MaxUses:          req.MaxUses,
	})
	if err != nil {
		return SubscriptionPromo{}, err
	}
	s.audit(ctx, actor, promo.ID, actionUpdate, req)
	return *promo, nil
}

func (s *Service) ValidatePromo(ctx context.Context, req ValidatePromoRequest) PromoValidation {
	invalid := func(msg string) PromoValidation {
		return PromoValidation{Valid: false, PromoCode: req.PromoCode, Message: msg}
	}
	promo, err := s.repository.FindPromoByCode(ctx, req.PromoCode)
	if err != nil || !promo.IsActive {
		return invalid("Promo code not found or inactive")
	}
	today := time.Now().Format("2006-01-02")
	if promo.ValidFrom > today || promo.ValidUntil < today {
		return invalid("Promo code is expired or not yet valid")
	}
	if promo.MaxUses != nil && promo.UsedCount >= *promo.MaxUses {
		return invalid("Promo code usage limit reached")
	}
	if len(promo.ApplicablePlans) > 0 && !containsStr(promo.ApplicablePlans, strings.ToUpper(req.PlanCode)) {
		return invalid("Promo not applicable to this plan")
	}
	cycle, ok := canonicalCycle(req.BillingCycle)
	if !ok || cycle == "TRIAL" {
		return invalid("Unknown billing cycle")
	}
	if len(promo.ApplicableCycles) > 0 && !containsStr(promo.ApplicableCycles, cycle) {
		return invalid("Promo not applicable to this billing cycle")
	}
	// Compute base price from plan
	plan, err := s.repository.FindPlanByCode(ctx, req.PlanCode)
	if err != nil {
		return invalid("Plan not found")
	}
	_, basePrice, _ := cycleEndAndAmount(time.Now(), cycle, *plan)
	savings := computeDiscount(basePrice, promo.DiscountType, promo.DiscountValue, promo.MaxDiscountCap)
	final := basePrice - savings
	if final < 0 {
		final = 0
	}
	gst, total := withGST(final)
	return PromoValidation{
		GSTAmount:     gst,
		TotalPrice:    total,
		Valid:         true,
		PromoCode:     promo.PromoCode,
		DiscountType:  promo.DiscountType,
		DiscountValue: promo.DiscountValue,
		BasePrice:     basePrice,
		Savings:       savings,
		FinalPrice:    final,
	}
}

func (s *Service) BlastPromo(ctx context.Context, actor ActorContext, promoID int64) (int, error) {
	if !actor.HasRole("ADMIN") {
		return 0, ErrForbidden
	}
	promo, err := s.repository.FindPromo(ctx, promoID)
	if err != nil {
		return 0, err
	}
	ownerIDs, err := s.repository.FindUnsubscribedOwnerIDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ownerIDs) == 0 {
		return 0, nil
	}
	title := "🌿 " + promo.Name
	message := buildBlastMessage(promo)
	dataMap := map[string]any{
		"promo_code":     promo.PromoCode,
		"discount_type":  promo.DiscountType,
		"discount_value": promo.DiscountValue,
		"valid_until":    promo.ValidUntil,
		"screen":         "subscription_payment",
	}
	dataJSON, _ := json.Marshal(dataMap)
	inputs := make([]BulkNotificationInput, 0, len(ownerIDs))
	for _, uid := range ownerIDs {
		inputs = append(inputs, BulkNotificationInput{
			UserID:   uid,
			Type:     "PROMO_OFFER",
			Title:    title,
			Message:  message,
			DataJSON: string(dataJSON),
		})
	}
	count, err := s.repository.BulkCreateNotifications(ctx, inputs)
	if err != nil {
		return 0, err
	}
	s.audit(ctx, actor, promoID, actionInsert, map[string]any{"blast_sent": count, "promo_id": promoID})
	return count, nil
}

func (s *Service) applyPromoDiscount(ctx context.Context, promoCode *string, planCode string, cycle string, amount float64) (float64, int64, error) {
	if promoCode == nil || strings.TrimSpace(*promoCode) == "" {
		return amount, 0, nil
	}
	promo, err := s.repository.FindPromoByCode(ctx, *promoCode)
	if err != nil || !promo.IsActive {
		return amount, 0, ErrInvalidInput
	}
	today := time.Now().Format("2006-01-02")
	if promo.ValidFrom > today || promo.ValidUntil < today {
		return amount, 0, ErrInvalidInput
	}
	if promo.MaxUses != nil && promo.UsedCount >= *promo.MaxUses {
		return amount, 0, ErrInvalidInput
	}
	if len(promo.ApplicablePlans) > 0 && !containsStr(promo.ApplicablePlans, strings.ToUpper(planCode)) {
		return amount, 0, ErrInvalidInput
	}
	if len(promo.ApplicableCycles) > 0 && !containsStr(promo.ApplicableCycles, strings.ToUpper(cycle)) {
		return amount, 0, ErrInvalidInput
	}
	savings := computeDiscount(amount, promo.DiscountType, promo.DiscountValue, promo.MaxDiscountCap)
	final := amount - savings
	if final < 0 {
		final = 0
	}
	return final, promo.ID, nil
}

func computeDiscount(base float64, discountType string, value float64, cap *float64) float64 {
	var savings float64
	if strings.ToUpper(discountType) == "PERCENTAGE" {
		savings = base * value / 100
		if cap != nil && savings > *cap {
			savings = *cap
		}
	} else {
		savings = value
	}
	if savings > base {
		savings = base
	}
	// To the paisa, so the shown total, the Razorpay order and the stored
	// payment agree (10% of ₹1,422.15 is ₹142.22, not ₹142.215).
	return math.Round(savings*100) / 100
}

func buildBlastMessage(promo *SubscriptionPromo) string {
	var discountStr string
	if promo.DiscountType == "PERCENTAGE" {
		discountStr = fmt.Sprintf("%.0f%% off", promo.DiscountValue)
	} else {
		discountStr = fmt.Sprintf("₹%.0f off", promo.DiscountValue)
	}
	return fmt.Sprintf("Use code %s to get %s on your subscription. Valid until %s.", promo.PromoCode, discountStr, promo.ValidUntil)
}

func validatePromoRequest(code, discountType string, value float64, validFrom, validUntil string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(discountType) == "" {
		return ErrInvalidInput
	}
	dt := strings.ToUpper(discountType)
	if dt != "PERCENTAGE" && dt != "FLAT" {
		return ErrInvalidInput
	}
	if value <= 0 {
		return ErrInvalidInput
	}
	if validFrom == "" || validUntil == "" || validFrom > validUntil {
		return ErrInvalidInput
	}
	return nil
}

func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, val) {
			return true
		}
	}
	return false
}

func (s *Service) UpdatePlan(ctx context.Context, actor ActorContext, planID int64, input UpdatePlanRequest) (SubscriptionPlan, error) {
	if !actor.HasRole("ADMIN") {
		return SubscriptionPlan{}, ErrForbidden
	}
	if input.MonthlyPrice < 0 {
		return SubscriptionPlan{}, ErrInvalidInput
	}
	yearly, _ := termFor("YEARLY")
	plan, err := s.repository.UpdatePlan(ctx, planID, UpdatePlanInput{
		Name:         input.Name,
		Description:  input.Description,
		MonthlyPrice: input.MonthlyPrice,
		YearlyPrice:  cyclePrice(input.MonthlyPrice, yearly), // kept in step for reports
		MaxManagers:  input.MaxManagers,
		IsActive:     input.IsActive,
		Features:     input.Features,
	})
	if err != nil {
		return SubscriptionPlan{}, err
	}
	s.invalidatePlanCache(ctx)
	return *plan, nil
}

// isFreePlan: plans an owner may not choose themselves.
func isFreePlan(code string) bool {
	return code == "TRIAL" || code == "FREE"
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

// calendarDaysLeft counts whole days from today to endDate (a DATE, valid
// through its last day): ends tomorrow -> 1, ends today -> 0. Dividing the
// remaining hours by 24 under-counted by one for most of the day.
func calendarDaysLeft(endDate, now time.Time) int {
	day := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return int(day(endDate).Sub(day(now)).Hours() / 24)
}
