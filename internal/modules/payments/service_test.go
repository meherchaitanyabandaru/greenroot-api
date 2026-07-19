package payments

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	payments      map[int64]*Payment
	orderAccess   map[int64]*OrderAccess
	subAccess     map[int64]*SubscriptionAccess
	nurseryMember map[string]bool // "nurseryID:userID"
	nextID        int64
}

func newMock() *mockRepo {
	return &mockRepo{
		payments:      make(map[int64]*Payment),
		orderAccess:   make(map[int64]*OrderAccess),
		subAccess:     make(map[int64]*SubscriptionAccess),
		nurseryMember: make(map[string]bool),
		nextID:        100,
	}
}

func (m *mockRepo) next() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedOrder(orderID int64, buyerID *int64, nurseryID *int64) {
	m.orderAccess[orderID] = &OrderAccess{OrderID: orderID, BuyerID: buyerID, NurseryID: nurseryID}
}

func (m *mockRepo) seedSub(subID, userID int64) {
	m.subAccess[subID] = &SubscriptionAccess{SubscriptionID: subID, UserID: userID}
}

func (m *mockRepo) seedNurseryMember(nurseryID, userID int64) {
	key := mkMemberKey(nurseryID, userID)
	m.nurseryMember[key] = true
}

func mkMemberKey(nurseryID, userID int64) string {
	return fmt.Sprintf("%d:%d", nurseryID, userID)
}

func (m *mockRepo) List(_ context.Context, _ ListPaymentsRequest) ([]Payment, int64, error) {
	result := make([]Payment, 0, len(m.payments))
	for _, p := range m.payments {
		result = append(result, *p)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, paymentID int64) (*Payment, error) {
	p, ok := m.payments[paymentID]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockRepo) Create(_ context.Context, input CreatePaymentInput) (*Payment, error) {
	id := m.next()
	p := &Payment{
		ID:                 id,
		PaymentFor:         input.PaymentFor,
		OrderID:            input.OrderID,
		UserSubscriptionID: input.UserSubscriptionID,
		PayerUserID:        input.PayerUserID,
		Amount:             input.Amount,
		Status:             input.Status,
	}
	m.payments[id] = p
	return p, nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, paymentID int64, input UpdatePaymentInput) (*Payment, error) {
	p, ok := m.payments[paymentID]
	if !ok {
		return nil, ErrNotFound
	}
	p.Status = input.Status
	return p, nil
}

func (m *mockRepo) OrderAccess(_ context.Context, orderID int64) (*OrderAccess, error) {
	a, ok := m.orderAccess[orderID]
	if !ok {
		return &OrderAccess{OrderID: orderID}, nil
	}
	return a, nil
}

func (m *mockRepo) SubscriptionAccess(_ context.Context, subID int64) (*SubscriptionAccess, error) {
	a, ok := m.subAccess[subID]
	if !ok {
		return nil, ErrNotFound
	}
	return a, nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.nurseryMember[mkMemberKey(nurseryID, userID)], nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func buyerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}

func ptr[T any](v T) *T { return &v }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── CreateManual ─────────────────────────────────────────────────────────────

func TestCreateManual_AdminOrderPayment(t *testing.T) {
	repo := newMock()
	repo.seedOrder(10, ptr(int64(20)), ptr(int64(1)))

	p, err := svc(repo).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        500.0,
		PaymentMethod: "CASH",
		Status:        "SUCCESS",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Amount != 500.0 {
		t.Errorf("want amount 500.0, got %v", p.Amount)
	}
}

func TestCreateManual_BuyerCanPayOwnOrder(t *testing.T) {
	repo := newMock()
	repo.seedOrder(10, ptr(int64(20)), nil) // buyer 20 owns order 10

	_, err := svc(repo).CreateManual(context.Background(), buyerActor(20), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        200.0,
		PaymentMethod: "UPI",
		Status:        "SUCCESS",
	})
	if err != nil {
		t.Fatalf("buyer of order should pay: %v", err)
	}
}

func TestCreateManual_BuyerCannotPayOtherOrder(t *testing.T) {
	repo := newMock()
	repo.seedOrder(10, ptr(int64(99)), nil) // buyer 99 owns order 10

	_, err := svc(repo).CreateManual(context.Background(), buyerActor(20), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        200.0,
		PaymentMethod: "UPI",
		Status:        "SUCCESS",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for other buyer, got %v", err)
	}
}

func TestCreateManual_ZeroAmountInvalid(t *testing.T) {
	repo := newMock()
	repo.seedOrder(10, nil, nil)

	_, err := svc(repo).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        0,
		PaymentMethod: "CASH",
		Status:        "SUCCESS",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for amount=0, got %v", err)
	}
}

func TestCreateManual_NegativeAmountInvalid(t *testing.T) {
	_, err := svc(newMock()).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        -50.0,
		PaymentMethod: "CASH",
		Status:        "SUCCESS",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for negative amount, got %v", err)
	}
}

func TestCreateManual_BadStatusInvalid(t *testing.T) {
	repo := newMock()
	repo.seedOrder(10, nil, nil)

	_, err := svc(repo).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        100.0,
		PaymentMethod: "CASH",
		Status:        "BOUNCED",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad status, got %v", err)
	}
}

func TestCreateManual_BadMethodInvalid(t *testing.T) {
	repo := newMock()
	repo.seedOrder(10, nil, nil)

	_, err := svc(repo).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        100.0,
		PaymentMethod: "BARTER",
		Status:        "SUCCESS",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad method, got %v", err)
	}
}

func TestCreateManual_OrderForWithoutOrderIDInvalid(t *testing.T) {
	_, err := svc(newMock()).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		Amount:        100.0,
		PaymentMethod: "CASH",
		Status:        "SUCCESS",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for ORDER without OrderID, got %v", err)
	}
}

func TestCreateManual_SubscriptionPayment(t *testing.T) {
	repo := newMock()
	repo.seedSub(5, 20) // subscription 5 belongs to user 20

	_, err := svc(repo).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:         "SUBSCRIPTION",
		UserSubscriptionID: ptr(int64(5)),
		Amount:             999.0,
		PaymentMethod:      "UPI",
		Status:             "SUCCESS",
	})
	if err != nil {
		t.Fatalf("subscription payment should succeed: %v", err)
	}
}

func TestCreateManual_DefaultStatusIsSuccess(t *testing.T) {
	repo := newMock()
	repo.seedOrder(10, nil, nil)

	p, err := svc(repo).CreateManual(context.Background(), adminActor(1), ManualPaymentRequest{
		PaymentFor:    "ORDER",
		OrderID:       ptr(int64(10)),
		Amount:        100.0,
		PaymentMethod: "CASH",
		// Status intentionally omitted → defaults to SUCCESS
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status != "SUCCESS" {
		t.Errorf("want default status SUCCESS, got %s", p.Status)
	}
}

// ─── UpdateStatus ─────────────────────────────────────────────────────────────

func TestUpdateStatus_AdminSuccess(t *testing.T) {
	repo := newMock()
	orderID := int64(10)
	payerID := int64(20)
	repo.payments[1] = &Payment{ID: 1, PaymentFor: "ORDER", OrderID: &orderID, PayerUserID: &payerID, Status: "PENDING"}
	repo.seedOrder(10, &payerID, nil)

	p, err := svc(repo).UpdateStatus(context.Background(), adminActor(1), 1, UpdateStatusRequest{Status: "SUCCESS"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status != "SUCCESS" {
		t.Errorf("want SUCCESS, got %s", p.Status)
	}
}

func TestUpdateStatus_BadStatusInvalid(t *testing.T) {
	repo := newMock()
	orderID := int64(10)
	repo.payments[1] = &Payment{ID: 1, PaymentFor: "ORDER", OrderID: &orderID, Status: "PENDING"}

	_, err := svc(repo).UpdateStatus(context.Background(), adminActor(1), 1, UpdateStatusRequest{Status: "BOUNCED"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad status, got %v", err)
	}
}

func TestUpdateStatus_NotFound(t *testing.T) {
	_, err := svc(newMock()).UpdateStatus(context.Background(), adminActor(1), 9999, UpdateStatusRequest{Status: "SUCCESS"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestUpdateStatus_BuyerCanUpdateOwnOrderPayment(t *testing.T) {
	repo := newMock()
	orderID := int64(10)
	payerID := int64(20)
	repo.payments[1] = &Payment{ID: 1, PaymentFor: "ORDER", OrderID: &orderID, PayerUserID: &payerID, Status: "PENDING"}
	repo.seedOrder(10, &payerID, nil)

	_, err := svc(repo).UpdateStatus(context.Background(), buyerActor(20), 1, UpdateStatusRequest{Status: "SUCCESS"})
	if err != nil {
		t.Fatalf("buyer should update own order payment: %v", err)
	}
}

func TestUpdateStatus_BuyerCannotUpdateOtherOrderPayment(t *testing.T) {
	repo := newMock()
	orderID := int64(10)
	otherBuyer := int64(99)
	repo.payments[1] = &Payment{ID: 1, PaymentFor: "ORDER", OrderID: &orderID, PayerUserID: &otherBuyer, Status: "PENDING"}
	repo.seedOrder(10, &otherBuyer, nil) // buyer 99 owns order

	_, err := svc(repo).UpdateStatus(context.Background(), buyerActor(20), 1, UpdateStatusRequest{Status: "SUCCESS"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for other buyer, got %v", err)
	}
}

// ─── isAllowedStatus / isAllowedMethod ───────────────────────────────────────

func TestIsAllowedStatus(t *testing.T) {
	for _, s := range []string{"PENDING", "SUCCESS", "FAILED", "REFUNDED", "CANCELLED"} {
		if !isAllowedStatus(s) {
			t.Errorf("status %q should be allowed", s)
		}
	}
	for _, s := range []string{"BOUNCED", "UNKNOWN", ""} {
		if isAllowedStatus(s) {
			t.Errorf("status %q should NOT be allowed", s)
		}
	}
}

func TestIsAllowedMethod(t *testing.T) {
	for _, m := range []string{"UPI", "CARD", "CASH", "BANK_TRANSFER", "NET_BANKING", "WALLET", "COD", "CHEQUE", "OTHER"} {
		if !isAllowedMethod(m) {
			t.Errorf("method %q should be allowed", m)
		}
	}
	if isAllowedMethod("BARTER") {
		t.Error("BARTER should NOT be allowed")
	}
}
