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
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("conflict")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	redis      redis.Cmdable
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
	if !hasRole(actor, "ADMIN") && input.UserID != actor.UserID {
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
	cycle := strings.ToUpper(strings.TrimSpace(input.BillingCycle))
	endDate, amount, err := cycleEndAndAmount(startDate, cycle, *plan)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	finalAmount, promoID, err := s.applyPromoDiscount(ctx, input.PromoCode, plan.Code, cycle, amount)
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
	if err := s.createSubscriptionPayment(ctx, subscription.ID, userID, finalAmount, input.PaymentMethod, input.Provider, input.ProviderRef, "Subscription created"); err != nil {
		return UserSubscription{}, err
	}
	if promoID > 0 {
		_ = s.repository.IncrementPromoUsed(ctx, promoID)
	}
	s.scheduleSubscriptionExpiry(ctx, subscription)
	s.audit(ctx, actor, subscription.ID, actionInsert, map[string]any{"plan_id": input.PlanID, "user_id": userID, "billing_cycle": input.BillingCycle, "promo_code": input.PromoCode})
	return enrichSubscription(actor, *subscription), nil
}

func (s *Service) Me(ctx context.Context, actor ActorContext) ([]UserSubscription, Pagination, error) {
	// Managers are nursery employees and do not hold subscriptions.
	if hasRole(actor, "MANAGER") {
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
	plan, err := s.repository.FindPlan(ctx, current.PlanID)
	if err != nil {
		return UserSubscription{}, err
	}
	startDate := time.Now().Truncate(24 * time.Hour)
	if current.EndDate != nil && current.EndDate.After(startDate) {
		startDate = current.EndDate.AddDate(0, 0, 1)
	}
	cycle := strings.ToUpper(strings.TrimSpace(input.BillingCycle))
	endDate, amount, err := cycleEndAndAmount(startDate, cycle, *plan)
	if err != nil {
		return UserSubscription{}, ErrInvalidInput
	}
	finalAmount, promoID, err := s.applyPromoDiscount(ctx, input.PromoCode, current.PlanCode, cycle, amount)
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
	if err := s.createSubscriptionPayment(ctx, subscription.ID, current.UserID, finalAmount, input.PaymentMethod, input.Provider, input.ProviderRef, "Subscription renewed"); err != nil {
		return UserSubscription{}, err
	}
	if promoID > 0 {
		_ = s.repository.IncrementPromoUsed(ctx, promoID)
	}
	s.scheduleSubscriptionExpiry(ctx, subscription)
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
	subscription, err := s.repository.UpdateStatus(ctx, subscriptionID, statusCancelled)
	if err != nil {
		return UserSubscription{}, err
	}
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

// CreateTrialForOwner auto-creates a 6-month TRIAL subscription when a nursery is approved.
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
	startDateStr := approvalDate.Truncate(24 * time.Hour).Format(time.DateOnly)
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
	daysRemaining := subscription.DaysRemaining
	if daysRemaining == nil && subscription.EndDate != nil {
		days := int(subscription.EndDate.Sub(time.Now()).Hours() / 24)
		if subscription.EndDate.After(time.Now()) && days == 0 {
			days = 1
		}
		daysRemaining = &days
		subscription.DaysRemaining = daysRemaining
	}
	isAdmin := hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN")
	isOwner := subscription.UserID == actor.UserID
	isActive := status == statusActive
	isExpired := status == statusExpired || (daysRemaining != nil && *daysRemaining < 0)
	isExpiringSoon := isActive && daysRemaining != nil && *daysRemaining >= 0 && *daysRemaining <= 14
	paymentStatus := ""
	canRetryPayment := false
	if subscription.LatestPayment != nil {
		paymentStatus = strings.ToUpper(strings.TrimSpace(subscription.LatestPayment.Status))
		canRetryPayment = paymentStatus == "FAILED" || paymentStatus == "PENDING"
	}

	subscription.Lifecycle = lifecyclePtr(lifecycle.Subscription(status, daysRemaining))
	subscription.Summary = &SubscriptionSummary{
		IsActive:       isActive,
		IsExpired:      isExpired,
		IsExpiringSoon: isExpiringSoon,
		PaymentStatus:  paymentStatus,
	}
	subscription.Capabilities = &SubscriptionCapabilities{
		CanRenew:        isOwner || isAdmin,
		CanCancel:       (isOwner || isAdmin) && (status == statusActive || status == statusPaused),
		CanPause:        isAdmin && status == statusActive,
		CanResume:       isAdmin && status == statusPaused,
		CanChangePlan:   isOwner || isAdmin,
		CanRetryPayment: (isOwner || isAdmin) && canRetryPayment,
	}
	return subscription
}

func lifecyclePtr[T any](value T) *T {
	return &value
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
	case "SIX_MONTH", "SEMI_ANNUAL", "", "MONTHLY": // MONTHLY/empty kept for backward compat
		amount := float64OrZero(plan.MonthlyPrice)
		return startDate.AddDate(0, 6, -1), amount, nil
	case "YEARLY", "ANNUAL":
		amount := float64OrZero(plan.YearlyPrice)
		return startDate.AddDate(1, 0, -1), amount, nil
	case "TRIAL":
		return startDate.AddDate(0, 6, -1), 0, nil
	default:
		return time.Time{}, 0, ErrInvalidInput
	}
}

// ── Promo service methods ─────────────────────────────────────────────────────

func (s *Service) ListPromos(ctx context.Context, actor ActorContext) ([]SubscriptionPromo, error) {
	if !hasRole(actor, "ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.ListPromos(ctx, false)
}

func (s *Service) CreatePromo(ctx context.Context, actor ActorContext, req CreatePromoRequest) (SubscriptionPromo, error) {
	if !hasRole(actor, "ADMIN") {
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
	if !hasRole(actor, "ADMIN") {
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
	cycle := strings.ToUpper(req.BillingCycle)
	if len(promo.ApplicableCycles) > 0 && !containsStr(promo.ApplicableCycles, cycle) {
		return invalid("Promo not applicable to this billing cycle")
	}
	// Compute base price from plan
	plan, err := s.repository.FindPlanByCode(ctx, req.PlanCode)
	if err != nil {
		return invalid("Plan not found")
	}
	var basePrice float64
	if cycle == "YEARLY" || cycle == "ANNUAL" {
		basePrice = float64OrZero(plan.YearlyPrice)
	} else {
		basePrice = float64OrZero(plan.MonthlyPrice)
	}
	savings := computeDiscount(basePrice, promo.DiscountType, promo.DiscountValue, promo.MaxDiscountCap)
	final := basePrice - savings
	if final < 0 {
		final = 0
	}
	return PromoValidation{
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
	if !hasRole(actor, "ADMIN") {
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
	return savings
}

func buildBlastMessage(promo *SubscriptionPromo) string {
	discountStr := ""
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
	if !hasRole(actor, "ADMIN") {
		return SubscriptionPlan{}, ErrForbidden
	}
	plan, err := s.repository.UpdatePlan(ctx, planID, UpdatePlanInput{
		Name:          input.Name,
		Description:   input.Description,
		SixMonthPrice: input.SixMonthPrice,
		YearlyPrice:   input.YearlyPrice,
		MaxManagers:   input.MaxManagers,
		IsActive:      input.IsActive,
		Features:      input.Features,
	})
	if err != nil {
		return SubscriptionPlan{}, err
	}
	s.invalidatePlanCache(ctx)
	return *plan, nil
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
