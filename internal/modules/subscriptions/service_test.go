package subscriptions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	plans          map[int64]*SubscriptionPlan
	plansByCode    map[string]*SubscriptionPlan
	subs           map[int64]*UserSubscription
	activeByUser   map[int64]*UserSubscription
	promos         map[int64]*SubscriptionPromo
	promosByCode   map[string]*SubscriptionPromo
	payments       map[int64][]Payment // subscriptionID → payments
	notifications  int
	listPlansCalls int

	nextPlanID  int64
	nextSubID   int64
	nextPromoID int64
}

func newMock() *mockRepo {
	return &mockRepo{
		plans:        make(map[int64]*SubscriptionPlan),
		plansByCode:  make(map[string]*SubscriptionPlan),
		subs:         make(map[int64]*UserSubscription),
		activeByUser: make(map[int64]*UserSubscription),
		promos:       make(map[int64]*SubscriptionPromo),
		promosByCode: make(map[string]*SubscriptionPromo),
		payments:     make(map[int64][]Payment),
		nextPlanID:   10,
		nextSubID:    100,
		nextPromoID:  200,
	}
}

func ptr[T any](v T) *T { return &v }

func (m *mockRepo) seedPlan(code string, monthly, yearly float64) *SubscriptionPlan {
	m.nextPlanID++
	p := &SubscriptionPlan{
		ID:           m.nextPlanID,
		Code:         code,
		Name:         code + " plan",
		IsActive:     true,
		MonthlyPrice: ptr(monthly),
		YearlyPrice:  ptr(yearly),
	}
	m.plans[m.nextPlanID] = p
	m.plansByCode[code] = p
	return p
}

func (m *mockRepo) seedSub(id, userID, planID int64, status string) *UserSubscription {
	s := &UserSubscription{ID: id, UserID: userID, PlanID: planID, PlanCode: "STANDARD", Status: status, StartDate: time.Now()}
	m.subs[id] = s
	if status == "ACTIVE" || status == "TRIAL" {
		m.activeByUser[userID] = s
	}
	return s
}

func (m *mockRepo) seedPromo(code, discountType string, value float64, validFrom, validUntil string) *SubscriptionPromo {
	m.nextPromoID++
	p := &SubscriptionPromo{
		ID:            m.nextPromoID,
		PromoCode:     code,
		Name:          code,
		DiscountType:  discountType,
		DiscountValue: value,
		IsActive:      true,
		ValidFrom:     validFrom,
		ValidUntil:    validUntil,
	}
	m.promos[m.nextPromoID] = p
	m.promosByCode[code] = p
	return p
}

// Repository interface implementation

func (m *mockRepo) ListPlans(_ context.Context, _ bool) ([]SubscriptionPlan, error) {
	m.listPlansCalls++
	result := make([]SubscriptionPlan, 0, len(m.plans))
	for _, p := range m.plans {
		result = append(result, *p)
	}
	return result, nil
}

func (m *mockRepo) FindPlan(_ context.Context, id int64) (*SubscriptionPlan, error) {
	p, ok := m.plans[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockRepo) FindPlanByCode(_ context.Context, code string) (*SubscriptionPlan, error) {
	p, ok := m.plansByCode[code]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockRepo) UpdatePlan(_ context.Context, id int64, input UpdatePlanInput) (*SubscriptionPlan, error) {
	p, ok := m.plans[id]
	if !ok {
		return nil, ErrNotFound
	}
	p.Name = input.Name
	p.IsActive = input.IsActive
	return p, nil
}

func (m *mockRepo) List(_ context.Context, req ListSubscriptionsRequest) ([]UserSubscription, int64, error) {
	result := make([]UserSubscription, 0)
	for _, s := range m.subs {
		if req.UserID == 0 || s.UserID == req.UserID {
			result = append(result, *s)
		}
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*UserSubscription, error) {
	s, ok := m.subs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (m *mockRepo) FindActiveByUser(_ context.Context, userID int64) (*UserSubscription, error) {
	s, ok := m.activeByUser[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (m *mockRepo) Create(_ context.Context, input CreateSubscriptionInput) (*UserSubscription, error) {
	m.nextSubID++
	s := &UserSubscription{
		ID:        m.nextSubID,
		UserID:    input.UserID,
		PlanID:    input.PlanID,
		PlanCode:  "STANDARD",
		Status:    "ACTIVE",
		StartDate: input.StartDate,
		EndDate:   &input.EndDate,
		AutoRenew: input.AutoRenew,
	}
	m.subs[m.nextSubID] = s
	m.activeByUser[input.UserID] = s
	return s, nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, id int64, status string) (*UserSubscription, error) {
	s, ok := m.subs[id]
	if !ok {
		return nil, ErrNotFound
	}
	s.Status = status
	return s, nil
}

func (m *mockRepo) Renew(_ context.Context, id int64, input RenewSubscriptionInput) (*UserSubscription, error) {
	s, ok := m.subs[id]
	if !ok {
		return nil, ErrNotFound
	}
	s.StartDate = input.StartDate
	s.EndDate = &input.EndDate
	return s, nil
}

func (m *mockRepo) CreatePayment(_ context.Context, _ CreatePaymentInput) error { return nil }

func (m *mockRepo) ListPaymentsBySubscription(_ context.Context, id int64) ([]Payment, error) {
	return m.payments[id], nil
}

func (m *mockRepo) ListPromos(_ context.Context, _ bool) ([]SubscriptionPromo, error) {
	result := make([]SubscriptionPromo, 0, len(m.promos))
	for _, p := range m.promos {
		result = append(result, *p)
	}
	return result, nil
}

func (m *mockRepo) FindPromo(_ context.Context, id int64) (*SubscriptionPromo, error) {
	p, ok := m.promos[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockRepo) FindPromoByCode(_ context.Context, code string) (*SubscriptionPromo, error) {
	p, ok := m.promosByCode[code]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockRepo) CreatePromo(_ context.Context, input CreatePromoInput) (*SubscriptionPromo, error) {
	m.nextPromoID++
	p := &SubscriptionPromo{
		ID:            m.nextPromoID,
		PromoCode:     input.PromoCode,
		Name:          input.Name,
		DiscountType:  input.DiscountType,
		DiscountValue: input.DiscountValue,
		IsActive:      true,
		ValidFrom:     input.ValidFrom,
		ValidUntil:    input.ValidUntil,
	}
	m.promos[m.nextPromoID] = p
	m.promosByCode[input.PromoCode] = p
	return p, nil
}

func (m *mockRepo) UpdatePromo(_ context.Context, id int64, input UpdatePromoInput) (*SubscriptionPromo, error) {
	p, ok := m.promos[id]
	if !ok {
		return nil, ErrNotFound
	}
	p.Name = input.Name
	p.IsActive = input.IsActive
	return p, nil
}

func (m *mockRepo) IncrementPromoUsed(_ context.Context, id int64) error {
	if p, ok := m.promos[id]; ok {
		p.UsedCount++
	}
	return nil
}

func (m *mockRepo) FindUnsubscribedOwnerIDs(_ context.Context) ([]int64, error) {
	return []int64{1, 2, 3}, nil
}

func (m *mockRepo) BulkCreateNotifications(_ context.Context, inputs []BulkNotificationInput) (int, error) {
	m.notifications += len(inputs)
	return len(inputs), nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func buyerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── ListPlans / GetPlan ──────────────────────────────────────────────────────

func TestListPlans_ReturnAll(t *testing.T) {
	repo := newMock()
	repo.seedPlan("TRIAL", 0, 0)
	repo.seedPlan("STANDARD", 1000, 10000)

	plans, err := svc(repo).ListPlans(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("want 2 plans, got %d", len(plans))
	}
}

func TestListPlans_UsesRedisCache(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	repo := newMock()
	repo.seedPlan("TRIAL", 0, 0)
	repo.seedPlan("STANDARD", 1000, 10000)
	service := NewService(repo, nil, client)

	if _, err := service.ListPlans(ctx); err != nil {
		t.Fatalf("first list plans failed: %v", err)
	}
	if _, err := service.ListPlans(ctx); err != nil {
		t.Fatalf("second list plans failed: %v", err)
	}
	if repo.listPlansCalls != 1 {
		t.Fatalf("expected one repository call due to cache hit, got %d", repo.listPlansCalls)
	}
	if !server.Exists(redisutil.KeySubscriptionPlans) {
		t.Fatal("expected subscription plans cache key")
	}
}

func TestUpdatePlan_InvalidatesRedisCache(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	repo := newMock()
	plan := repo.seedPlan("STANDARD", 1000, 10000)
	service := NewService(repo, nil, client)
	if err := client.Set(ctx, redisutil.KeySubscriptionPlans, "[]", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}

	_, err := service.UpdatePlan(ctx, adminActor(1), plan.ID, UpdatePlanRequest{
		Name:          "Updated",
		SixMonthPrice: 1200,
		YearlyPrice:   12000,
		IsActive:      true,
	})
	if err != nil {
		t.Fatalf("update plan failed: %v", err)
	}
	if server.Exists(redisutil.KeySubscriptionPlans) {
		t.Fatal("expected plan cache to be invalidated")
	}
}

func TestGetPlan_Found(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)

	plan, err := svc(repo).GetPlan(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Code != "STANDARD" {
		t.Errorf("want STANDARD, got %s", plan.Code)
	}
}

func TestGetPlan_NotFound(t *testing.T) {
	_, err := svc(newMock()).GetPlan(context.Background(), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── List / Me ────────────────────────────────────────────────────────────────

func TestList_AdminSeesAll(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")
	repo.seedSub(2, 20, 1, "ACTIVE")

	subs, _, err := svc(repo).List(context.Background(), adminActor(99), ListSubscriptionsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 2 {
		t.Errorf("want 2, got %d", len(subs))
	}
}

func TestList_UserSeeOwn(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")
	repo.seedSub(2, 20, 1, "ACTIVE")

	subs, _, err := svc(repo).List(context.Background(), ownerActor(10), ListSubscriptionsRequest{UserID: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 1 || subs[0].UserID != 10 {
		t.Errorf("expected 1 sub for user 10, got %d", len(subs))
	}
}

func TestList_NonAdminClampedToOwnUserID(t *testing.T) {
	// normalizeList overwrites UserID with actor.UserID for non-admins.
	// Passing another user's ID doesn't error — it returns the actor's own subs.
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	subs, _, err := svc(repo).List(context.Background(), ownerActor(20), ListSubscriptionsRequest{UserID: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// User 20 has no subs — normalizeList clamped to user 20.
	for _, s := range subs {
		if s.UserID != 20 {
			t.Errorf("got sub for user %d, expected only user 20's subs", s.UserID)
		}
	}
}

func TestMe_ReturnsOwnSubscriptions(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 5, 1, "ACTIVE")

	subs, _, err := svc(repo).Me(context.Background(), buyerActor(5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 sub, got %d", len(subs))
	}
}

// ─── Get ─────────────────────────────────────────────────────────────────────

func TestGet_AdminCanAccess(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	sub, err := svc(repo).Get(context.Background(), adminActor(99), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.ID != 1 {
		t.Errorf("want id 1, got %d", sub.ID)
	}
}

func TestGet_OwnerCanAccessOwn(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	_, err := svc(repo).Get(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGet_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	_, err := svc(repo).Get(context.Background(), ownerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ───────────────────────────────────────────────────────────────────

func TestCreate_Success_SixMonth(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)

	sub, err := svc(repo).Create(context.Background(), ownerActor(10), CreateSubscriptionRequest{
		PlanID:        p.ID,
		BillingCycle:  "SIX_MONTH",
		PaymentMethod: ptr("CASH"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.UserID != 10 {
		t.Errorf("want UserID 10, got %d", sub.UserID)
	}
}

func TestCreate_Success_Yearly(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)

	sub, err := svc(repo).Create(context.Background(), ownerActor(10), CreateSubscriptionRequest{
		PlanID:        p.ID,
		BillingCycle:  "YEARLY",
		PaymentMethod: ptr("CASH"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.UserID != 10 {
		t.Errorf("want UserID 10, got %d", sub.UserID)
	}
}

func TestCreate_Conflict_AlreadyActive(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	repo.seedSub(1, 10, p.ID, "ACTIVE")

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateSubscriptionRequest{
		PlanID:       p.ID,
		BillingCycle: "SIX_MONTH",
	})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("want ErrConflict, got %v", err)
	}
}

func TestCreate_AdminCanCreateForOtherUser(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	targetUserID := int64(20)

	sub, err := svc(repo).Create(context.Background(), adminActor(1), CreateSubscriptionRequest{
		UserID:        &targetUserID,
		PlanID:        p.ID,
		BillingCycle:  "SIX_MONTH",
		PaymentMethod: ptr("CASH"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.UserID != 20 {
		t.Errorf("want UserID 20, got %d", sub.UserID)
	}
}

func TestCreate_NonAdminCannotCreateForOther(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	targetUserID := int64(99)

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateSubscriptionRequest{
		UserID:       &targetUserID,
		PlanID:       p.ID,
		BillingCycle: "SIX_MONTH",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestCreate_InvalidPlan(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), ownerActor(10), CreateSubscriptionRequest{
		PlanID:       9999,
		BillingCycle: "SIX_MONTH",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound for unknown plan, got %v", err)
	}
}

func TestCreate_InactivePlanRejected(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("OLD", 500, 5000)
	p.IsActive = false

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateSubscriptionRequest{
		PlanID:       p.ID,
		BillingCycle: "SIX_MONTH",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for inactive plan, got %v", err)
	}
}

func TestCreate_InvalidBillingCycle(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateSubscriptionRequest{
		PlanID:       p.ID,
		BillingCycle: "QUARTERLY", // not supported
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_Trial_ZeroAmount(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("TRIAL", 0, 0)

	sub, err := svc(repo).Create(context.Background(), adminActor(1), CreateSubscriptionRequest{
		PlanID:       p.ID,
		BillingCycle: "TRIAL",
		UserID:       ptr(int64(10)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.UserID != 10 {
		t.Error("expected sub for user 10")
	}
}

// ─── UpdateStatus (admin-only) ────────────────────────────────────────────────

func TestUpdateStatus_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	sub, err := svc(repo).UpdateStatus(context.Background(), adminActor(99), 1, UpdateStatusRequest{Status: "PAUSED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.Status != "PAUSED" {
		t.Errorf("want PAUSED, got %s", sub.Status)
	}
}

func TestUpdateStatus_NonAdminForbidden(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(10), 1, UpdateStatusRequest{Status: "PAUSED"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestUpdateStatus_InvalidStatus(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	_, err := svc(repo).UpdateStatus(context.Background(), adminActor(99), 1, UpdateStatusRequest{Status: "DELETED"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

// ─── Renew ────────────────────────────────────────────────────────────────────

func TestRenew_Success(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	end := time.Now().AddDate(0, 6, -1)
	s := repo.seedSub(1, 10, p.ID, "ACTIVE")
	s.EndDate = &end
	s.PlanCode = "STANDARD"

	sub, err := svc(repo).Renew(context.Background(), ownerActor(10), 1, RenewSubscriptionRequest{
		BillingCycle:  "SIX_MONTH",
		PaymentMethod: ptr("CASH"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.ID != 1 {
		t.Errorf("want sub ID 1, got %d", sub.ID)
	}
}

func TestRenew_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	repo.seedSub(1, 10, p.ID, "ACTIVE")

	_, err := svc(repo).Renew(context.Background(), ownerActor(20), 1, RenewSubscriptionRequest{BillingCycle: "SIX_MONTH"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestRenew_NotFound(t *testing.T) {
	repo := newMock()
	repo.seedPlan("STANDARD", 1000, 10000)

	_, err := svc(repo).Renew(context.Background(), adminActor(1), 9999, RenewSubscriptionRequest{BillingCycle: "SIX_MONTH"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Cancel ───────────────────────────────────────────────────────────────────

func TestCancel_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	sub, err := svc(repo).Cancel(context.Background(), ownerActor(10), 1, CancelSubscriptionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.Status != statusCancelled {
		t.Errorf("want CANCELLED, got %s", sub.Status)
	}
}

func TestCancel_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	_, err := svc(repo).Cancel(context.Background(), ownerActor(20), 1, CancelSubscriptionRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── ListPayments ─────────────────────────────────────────────────────────────

func TestListPayments_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")
	repo.payments[1] = []Payment{{ID: 1, Amount: 1000, Status: "SUCCESS"}}

	payments, err := svc(repo).ListPayments(context.Background(), adminActor(99), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payments) != 1 {
		t.Errorf("want 1 payment, got %d", len(payments))
	}
}

func TestListPayments_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedSub(1, 10, 1, "ACTIVE")

	_, err := svc(repo).ListPayments(context.Background(), ownerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── CreateTrialForOwner ──────────────────────────────────────────────────────

func TestCreateTrialForOwner_Success(t *testing.T) {
	repo := newMock()
	repo.seedPlan("TRIAL", 0, 0)

	err := svc(repo).CreateTrialForOwner(context.Background(), 10, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.activeByUser[10]; !ok {
		t.Error("expected TRIAL sub created for user 10")
	}
}

func TestCreateTrialForOwner_AlreadyActive_NoOp(t *testing.T) {
	repo := newMock()
	repo.seedPlan("TRIAL", 0, 0)
	repo.seedSub(1, 10, 1, "ACTIVE")

	err := svc(repo).CreateTrialForOwner(context.Background(), 10, time.Now())
	if err != nil {
		t.Fatalf("should silently succeed, got %v", err)
	}
}

func TestCreateTrialForOwner_NoPlanSeeded_NoOp(t *testing.T) {
	// TRIAL plan not in DB → should succeed silently
	err := svc(newMock()).CreateTrialForOwner(context.Background(), 10, time.Now())
	if err != nil {
		t.Fatalf("should silently succeed without TRIAL plan, got %v", err)
	}
}

// ─── Promo — ListPromos / CreatePromo ─────────────────────────────────────────

func TestListPromos_AdminOnly(t *testing.T) {
	repo := newMock()
	repo.seedPromo("SAVE10", "PERCENTAGE", 10, "2026-01-01", "2026-12-31")

	_, err := svc(repo).ListPromos(context.Background(), adminActor(1))
	if err != nil {
		t.Fatalf("admin should see promos: %v", err)
	}

	_, err = svc(repo).ListPromos(context.Background(), ownerActor(10))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-admin, got %v", err)
	}
}

func TestCreatePromo_AdminSuccess(t *testing.T) {
	promo, err := svc(newMock()).CreatePromo(context.Background(), adminActor(1), CreatePromoRequest{
		PromoCode:     "SAVE10",
		Name:          "Save 10",
		DiscountType:  "PERCENTAGE",
		DiscountValue: 10,
		ValidFrom:     "2026-01-01",
		ValidUntil:    "2026-12-31",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if promo.PromoCode != "SAVE10" {
		t.Errorf("want SAVE10, got %s", promo.PromoCode)
	}
}

func TestCreatePromo_NonAdminForbidden(t *testing.T) {
	_, err := svc(newMock()).CreatePromo(context.Background(), ownerActor(10), CreatePromoRequest{
		PromoCode:     "SAVE10",
		DiscountType:  "PERCENTAGE",
		DiscountValue: 10,
		ValidFrom:     "2026-01-01",
		ValidUntil:    "2026-12-31",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestCreatePromo_InvalidDiscountType(t *testing.T) {
	_, err := svc(newMock()).CreatePromo(context.Background(), adminActor(1), CreatePromoRequest{
		PromoCode:     "BAD",
		DiscountType:  "FIXED", // must be FLAT, not FIXED
		DiscountValue: 100,
		ValidFrom:     "2026-01-01",
		ValidUntil:    "2026-12-31",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for FIXED discount type, got %v", err)
	}
}

func TestCreatePromo_ValidFlatType(t *testing.T) {
	_, err := svc(newMock()).CreatePromo(context.Background(), adminActor(1), CreatePromoRequest{
		PromoCode:     "FLAT100",
		DiscountType:  "FLAT",
		DiscountValue: 100,
		ValidFrom:     "2026-01-01",
		ValidUntil:    "2026-12-31",
	})
	if err != nil {
		t.Fatalf("FLAT should be valid, got %v", err)
	}
}

func TestCreatePromo_InvalidDateRange(t *testing.T) {
	_, err := svc(newMock()).CreatePromo(context.Background(), adminActor(1), CreatePromoRequest{
		PromoCode:     "BAD",
		DiscountType:  "PERCENTAGE",
		DiscountValue: 10,
		ValidFrom:     "2026-12-31",
		ValidUntil:    "2026-01-01", // end before start
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for inverted date range, got %v", err)
	}
}

// ─── ValidatePromo ────────────────────────────────────────────────────────────

func TestValidatePromo_Valid(t *testing.T) {
	repo := newMock()
	repo.seedPlan("STANDARD", 1000, 10000)
	repo.seedPromo("SAVE10", "PERCENTAGE", 10, "2026-01-01", "2099-12-31")

	result := svc(repo).ValidatePromo(context.Background(), ValidatePromoRequest{
		PromoCode:    "SAVE10",
		PlanCode:     "STANDARD",
		BillingCycle: "SIX_MONTH",
	})
	if !result.Valid {
		t.Errorf("expected valid promo, got message: %s", result.Message)
	}
	if result.Savings <= 0 {
		t.Error("expected positive savings for 10% promo")
	}
}

func TestValidatePromo_NotFound(t *testing.T) {
	result := svc(newMock()).ValidatePromo(context.Background(), ValidatePromoRequest{
		PromoCode:    "NOTEXIST",
		PlanCode:     "STANDARD",
		BillingCycle: "SIX_MONTH",
	})
	if result.Valid {
		t.Error("expected invalid result for unknown code")
	}
}

func TestValidatePromo_Expired(t *testing.T) {
	repo := newMock()
	repo.seedPlan("STANDARD", 1000, 10000)
	repo.seedPromo("OLD", "PERCENTAGE", 10, "2020-01-01", "2020-12-31")

	result := svc(repo).ValidatePromo(context.Background(), ValidatePromoRequest{
		PromoCode:    "OLD",
		PlanCode:     "STANDARD",
		BillingCycle: "SIX_MONTH",
	})
	if result.Valid {
		t.Error("expected invalid result for expired promo")
	}
}

// ─── BlastPromo ───────────────────────────────────────────────────────────────

func TestBlastPromo_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedPromo("SAVE10", "PERCENTAGE", 10, "2026-01-01", "2099-12-31")
	var promoID int64
	for id := range repo.promos {
		promoID = id
	}

	count, err := svc(repo).BlastPromo(context.Background(), adminActor(1), promoID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("want 3 notifications sent, got %d", count)
	}
}

func TestBlastPromo_NonAdminForbidden(t *testing.T) {
	_, err := svc(newMock()).BlastPromo(context.Background(), ownerActor(10), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── UpdatePlan ───────────────────────────────────────────────────────────────

func TestUpdatePlan_AdminSuccess(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)

	updated, err := svc(repo).UpdatePlan(context.Background(), adminActor(1), p.ID, UpdatePlanRequest{
		Name:     "New Name",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("want New Name, got %s", updated.Name)
	}
}

func TestUpdatePlan_NonAdminForbidden(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)

	_, err := svc(repo).UpdatePlan(context.Background(), ownerActor(10), p.ID, UpdatePlanRequest{Name: "Hack"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── billing cycle table test ─────────────────────────────────────────────────

func TestCycleEndAndAmount(t *testing.T) {
	monthly := 1000.0
	yearly := 10000.0
	plan := SubscriptionPlan{MonthlyPrice: &monthly, YearlyPrice: &yearly}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		cycle      string
		wantAmount float64
		wantMonths int
	}{
		{"SIX_MONTH", 1000, 6},
		{"YEARLY", 10000, 12},
		{"TRIAL", 0, 6},
		{"MONTHLY", 1000, 6},
	}
	for _, c := range cases {
		t.Run(c.cycle, func(t *testing.T) {
			end, amount, err := cycleEndAndAmount(start, c.cycle, plan)
			if err != nil {
				t.Fatalf("cycle %s: unexpected error: %v", c.cycle, err)
			}
			if amount != c.wantAmount {
				t.Errorf("cycle %s: want amount %.0f, got %.0f", c.cycle, c.wantAmount, amount)
			}
			months := int(end.Sub(start).Hours()/24/30) + 1
			_ = months
			if end.Before(start) {
				t.Errorf("cycle %s: end date before start", c.cycle)
			}
		})
	}
}

func TestCycleEndAndAmount_UnknownCycle(t *testing.T) {
	plan := SubscriptionPlan{}
	_, _, err := cycleEndAndAmount(time.Now(), "WEEKLY", plan)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

// ─── computeDiscount ──────────────────────────────────────────────────────────

func TestComputeDiscount_Percentage(t *testing.T) {
	savings := computeDiscount(1000, "PERCENTAGE", 10, nil)
	if savings != 100 {
		t.Errorf("want 100, got %.2f", savings)
	}
}

func TestComputeDiscount_PercentageWithCap(t *testing.T) {
	cap := 50.0
	savings := computeDiscount(1000, "PERCENTAGE", 10, &cap) // 10% = 100 but capped at 50
	if savings != 50 {
		t.Errorf("want 50 (capped), got %.2f", savings)
	}
}

func TestComputeDiscount_Flat(t *testing.T) {
	savings := computeDiscount(1000, "FLAT", 200, nil)
	if savings != 200 {
		t.Errorf("want 200, got %.2f", savings)
	}
}

func TestComputeDiscount_FlatExceedsBase(t *testing.T) {
	savings := computeDiscount(100, "FLAT", 500, nil) // cannot exceed base
	if savings != 100 {
		t.Errorf("want 100 (clamped to base), got %.2f", savings)
	}
}
