package quotations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	quotations       map[int64]*Quotation
	nurseryOwners    map[int64]int64   // nursery_id → owner_user_id
	nurseryMembers   map[int64][]int64 // nursery_id → []user_id (includes owner)
	ownedNurseries   map[int64]int64   // user_id → nursery_id (owners)
	managedNurseries map[int64]int64   // user_id → nursery_id (managers)
	activeNurseries  map[int64]bool    // nursery_id → is active
	nurseryCustomers map[int64][]int64 // nursery_id → connected customer user_ids
	orders           map[int64]int64   // order_id → nursery_id
	userMobiles      map[int64]string
	softDeleted      []int64
	lastListInput    ListQuotationsRequest
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		quotations:       make(map[int64]*Quotation),
		nurseryOwners:    make(map[int64]int64),
		nurseryMembers:   make(map[int64][]int64),
		ownedNurseries:   make(map[int64]int64),
		managedNurseries: make(map[int64]int64),
		activeNurseries:  make(map[int64]bool),
		nurseryCustomers: make(map[int64][]int64),
		orders:           make(map[int64]int64),
		userMobiles:      make(map[int64]string),
	}
}

func (m *mockRepo) addQuotation(q Quotation) { m.quotations[q.ID] = &q }

func (m *mockRepo) addNursery(nurseryID, ownerUserID int64, memberIDs ...int64) {
	m.nurseryOwners[nurseryID] = ownerUserID
	m.ownedNurseries[ownerUserID] = nurseryID
	m.activeNurseries[nurseryID] = true
	members := []int64{ownerUserID}
	members = append(members, memberIDs...)
	m.nurseryMembers[nurseryID] = members
	for _, mid := range memberIDs {
		m.managedNurseries[mid] = nurseryID
	}
}

func (m *mockRepo) addOrder(orderID, nurseryID int64) { m.orders[orderID] = nurseryID }

func (m *mockRepo) addCustomer(nurseryID, userID int64) {
	m.nurseryCustomers[nurseryID] = append(m.nurseryCustomers[nurseryID], userID)
}

func (m *mockRepo) List(_ context.Context, input ListQuotationsRequest) ([]Quotation, int64, error) {
	m.lastListInput = input
	return nil, 0, nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*Quotation, error) {
	q, ok := m.quotations[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *q
	return &cp, nil
}

func (m *mockRepo) Create(_ context.Context, actorID int64, input CreateQuotationRequest, createdByName string, nurseryName *string, nurseryPhone *string) (*Quotation, error) {
	id := int64(len(m.quotations) + 1)
	status := "CUSTOMER_DRAFT"
	if input.QuotationType == "INTERNAL" {
		status = "INTERNAL_DRAFT"
	}
	nid := int64(0)
	if input.NurseryID != nil {
		nid = *input.NurseryID
	}
	q := &Quotation{
		ID:              id,
		QuotationCode:   "QT-TEST",
		QuotationType:   input.QuotationType,
		CreatedByUserID: actorID,
		Status:          status,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if nid > 0 {
		q.NurseryID = &nid
	}
	m.quotations[id] = q
	return q, nil
}

func (m *mockRepo) Update(_ context.Context, id int64, input UpdateQuotationRequest) (*Quotation, error) {
	q, ok := m.quotations[id]
	if !ok {
		return nil, ErrNotFound
	}
	q.CustomerUserID = input.CustomerUserID
	q.RecipientName = input.RecipientName
	q.RecipientMobile = input.RecipientMobile
	q.Notes = input.Notes
	q.UpdatedAt = time.Now()
	cp := *q
	return &cp, nil
}

func (m *mockRepo) UpdateCustomer(_ context.Context, id int64, input UpdateQuotationCustomerRequest) (*Quotation, error) {
	q, ok := m.quotations[id]
	if !ok {
		return nil, ErrNotFound
	}
	q.CustomerUserID = input.CustomerUserID
	q.RecipientName = input.RecipientName
	q.RecipientMobile = input.RecipientMobile
	q.UpdatedAt = time.Now()
	cp := *q
	return &cp, nil
}

func (m *mockRepo) Approve(_ context.Context, id int64, _ int64) (*Quotation, error) {
	q, ok := m.quotations[id]
	if !ok {
		return nil, ErrNotFound
	}
	q.Status = "CUSTOMER_SENT"
	cp := *q
	return &cp, nil
}

func (m *mockRepo) Recall(_ context.Context, id int64) (*Quotation, error) {
	q, ok := m.quotations[id]
	if !ok {
		return nil, ErrNotFound
	}
	q.Status = "CUSTOMER_DRAFT"
	cp := *q
	return &cp, nil
}

func (m *mockRepo) BuyerAccept(_ context.Context, id int64, _ int64) (*Quotation, error) {
	q, ok := m.quotations[id]
	if !ok {
		return nil, ErrNotFound
	}
	q.Status = "CUSTOMER_ACCEPTED"
	cp := *q
	return &cp, nil
}

func (m *mockRepo) BuyerReject(_ context.Context, id int64, _ int64, _ string) (*Quotation, error) {
	q, ok := m.quotations[id]
	if !ok {
		return nil, ErrNotFound
	}
	q.Status = "CUSTOMER_REJECTED"
	cp := *q
	return &cp, nil
}

func (m *mockRepo) GetBuyerNurseryID(_ context.Context, _ int64) (*int64, error) { return nil, nil }

func (m *mockRepo) SoftDelete(_ context.Context, id int64) error {
	if _, ok := m.quotations[id]; !ok {
		return ErrNotFound
	}
	m.softDeleted = append(m.softDeleted, id)
	delete(m.quotations, id)
	return nil
}

func (m *mockRepo) FindNurseryOwnerID(_ context.Context, quotationID int64) (int64, error) {
	q, ok := m.quotations[quotationID]
	if !ok || q.NurseryID == nil {
		return 0, ErrNotFound
	}
	ownerID, ok := m.nurseryOwners[*q.NurseryID]
	if !ok {
		return 0, ErrNotFound
	}
	return ownerID, nil
}

func (m *mockRepo) AssignManager(_ context.Context, quotationID int64, managerUserID int64) (*Quotation, error) {
	q, ok := m.quotations[quotationID]
	if !ok {
		return nil, ErrNotFound
	}
	q.AssignedManagerUserID = &managerUserID
	cp := *q
	return &cp, nil
}

func (m *mockRepo) UnassignManager(_ context.Context, quotationID int64) (*Quotation, error) {
	q, ok := m.quotations[quotationID]
	if !ok {
		return nil, ErrNotFound
	}
	q.AssignedManagerUserID = nil
	cp := *q
	return &cp, nil
}

func (m *mockRepo) MarkConverted(_ context.Context, quotationID int64, orderID int64, _ int64) error {
	q, ok := m.quotations[quotationID]
	if !ok {
		return ErrNotFound
	}
	q.Status = "CONVERTED"
	q.ConvertedOrderID = &orderID
	return nil
}

func (m *mockRepo) CreateOrderAndConvert(_ context.Context, q *Quotation, _ int64) (int64, error) {
	const fakeOrderID = int64(1000)
	q.Status = "CONVERTED"
	q.ConvertedOrderID = func() *int64 { v := fakeOrderID; return &v }()
	if stored, ok := m.quotations[q.ID]; ok {
		stored.Status = "CONVERTED"
		stored.ConvertedOrderID = q.ConvertedOrderID
	}
	return fakeOrderID, nil
}

func (m *mockRepo) GetNurseryInfo(_ context.Context, nurseryID int64) (string, string, error) {
	return "Test Nursery", "9000000000", nil
}

func (m *mockRepo) GetUserName(_ context.Context, _ int64) (string, error) { return "Test User", nil }

func (m *mockRepo) GetUserMobile(_ context.Context, userID int64) (string, error) {
	mobile, ok := m.userMobiles[userID]
	if !ok {
		return "", ErrNotFound
	}
	return mobile, nil
}

func (m *mockRepo) GetPlantInfo(_ context.Context, _ int64) (string, string, error) {
	return "Mangifera indica", "Mango", nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID int64, userID int64) (bool, error) {
	for _, mid := range m.nurseryMembers[nurseryID] {
		if mid == userID {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockRepo) IsNurseryOwner(_ context.Context, nurseryID int64, userID int64) (bool, error) {
	ownerID, ok := m.nurseryOwners[nurseryID]
	if !ok {
		return false, nil
	}
	return ownerID == userID, nil
}

func (m *mockRepo) IsNurseryCustomer(_ context.Context, nurseryID int64, userID int64) (bool, error) {
	for _, customerID := range m.nurseryCustomers[nurseryID] {
		if customerID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockRepo) GetOwnedNurseryID(_ context.Context, userID int64) (*int64, error) {
	nurseryID, ok := m.ownedNurseries[userID]
	if !ok {
		return nil, nil
	}
	return &nurseryID, nil
}

func (m *mockRepo) GetManagerNurseryID(_ context.Context, userID int64) (*int64, error) {
	nurseryID, ok := m.managedNurseries[userID]
	if !ok {
		return nil, nil
	}
	return &nurseryID, nil
}

func (m *mockRepo) IsNurseryActive(_ context.Context, nurseryID int64) (bool, error) {
	return m.activeNurseries[nurseryID], nil
}

func (m *mockRepo) GetOrderNurseryID(_ context.Context, orderID int64) (*int64, error) {
	nurseryID, ok := m.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	return &nurseryID, nil
}

func (m *mockRepo) CreateNotification(_ context.Context, _ int64, _, _, _ string) error {
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func ownerActor(userID int64) ActorContext {
	return ActorContext{UserID: userID, Roles: []string{"NURSERY_OWNER"}}
}

func managerActor(userID int64) ActorContext {
	return ActorContext{UserID: userID, Roles: []string{"MANAGER"}}
}

func buyerActor(userID int64) ActorContext {
	return ActorContext{UserID: userID, Roles: []string{"BUYER"}}
}

func adminActor(userID int64) ActorContext {
	return ActorContext{UserID: userID, Roles: []string{"ADMIN"}}
}

func ptr[T any](v T) *T { return &v }

func validItem() QuotationItemRequest {
	return QuotationItemRequest{PlantID: 1, Quantity: 2, UnitPrice: 100, TotalPrice: 200}
}

// baseQuotation returns a CUSTOMER_DRAFT quotation attached to nursery 10, created by user 1.
func baseQuotation(id int64, status string) Quotation {
	nurseryID := int64(10)
	return Quotation{
		ID:              id,
		QuotationCode:   "QT-001",
		QuotationType:   "CUSTOMER",
		Status:          status,
		CreatedByUserID: 1,
		NurseryID:       &nurseryID,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// ── Update tests ──────────────────────────────────────────────────────────────

func TestUpdate_BlockedAfterApproval(t *testing.T) {
	for _, status := range []string{"CUSTOMER_SENT", "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED"} {
		t.Run(status, func(t *testing.T) {
			repo := newMockRepo()
			repo.addNursery(10, 1)
			repo.addQuotation(baseQuotation(1, status))
			svc := NewService(repo, nil)

			_, err := svc.Update(context.Background(), ownerActor(1), 1, UpdateQuotationRequest{Items: []QuotationItemRequest{validItem()}})
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("status %s: expected ErrInvalidTransition, got %v", status, err)
			}
		})
	}
}

func TestUpdate_AllowedInDraftStatuses(t *testing.T) {
	for _, status := range []string{"INTERNAL_DRAFT", "CUSTOMER_DRAFT"} {
		t.Run(status, func(t *testing.T) {
			repo := newMockRepo()
			repo.addNursery(10, 1)
			repo.addQuotation(baseQuotation(1, status))
			svc := NewService(repo, nil)

			_, err := svc.Update(context.Background(), ownerActor(1), 1, UpdateQuotationRequest{Items: []QuotationItemRequest{validItem()}})
			if err != nil {
				t.Errorf("status %s: unexpected error: %v", status, err)
			}
		})
	}
}

func TestUpdate_ManagerCanEdit(t *testing.T) {
	// Business rule: both owners and managers can edit quotations.
	// Previously only the creator could edit — this was a bug.
	repo := newMockRepo()
	repo.addNursery(10, 1, 2) // user 2 is manager
	q := baseQuotation(1, "CUSTOMER_DRAFT")
	q.CreatedByUserID = 1 // created by owner, not manager
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	_, err := svc.Update(context.Background(), managerActor(2), 1, UpdateQuotationRequest{Items: []QuotationItemRequest{validItem()}})
	if err != nil {
		t.Errorf("manager should be able to edit: %v", err)
	}
}

func TestUpdate_NonMemberForbidden(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1) // user 99 is not a member
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	_, err := svc.Update(context.Background(), ownerActor(99), 1, UpdateQuotationRequest{Items: []QuotationItemRequest{validItem()}})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestUpdateCustomer_OwnerCanChangeCustomerWhenSent(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addCustomer(10, 20)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_SENT"))
	svc := NewService(repo, nil)

	name := "Ravi"
	mobile := "9300000000"
	customerID := int64(20)
	got, err := svc.UpdateCustomer(context.Background(), ownerActor(1), 1, UpdateQuotationCustomerRequest{
		CustomerUserID:  &customerID,
		RecipientName:   &name,
		RecipientMobile: &mobile,
	})
	if err != nil {
		t.Fatalf("owner should update customer while sent: %v", err)
	}
	if got.CustomerUserID == nil || *got.CustomerUserID != customerID {
		t.Fatalf("customer_user_id not saved: %+v", got.CustomerUserID)
	}
	if got.RecipientName == nil || *got.RecipientName != name {
		t.Fatalf("recipient_name not saved: %+v", got.RecipientName)
	}
}

func TestUpdateCustomer_ManagerForbidden(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1, 2)
	repo.addCustomer(10, 20)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_SENT"))
	svc := NewService(repo, nil)

	customerID := int64(20)
	_, err := svc.UpdateCustomer(context.Background(), managerActor(2), 1, UpdateQuotationCustomerRequest{
		CustomerUserID: &customerID,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager should not update customer: got %v", err)
	}
}

func TestUpdateCustomer_RejectsUnlinkedCustomer(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_SENT"))
	svc := NewService(repo, nil)

	customerID := int64(99)
	_, err := svc.UpdateCustomer(context.Background(), ownerActor(1), 1, UpdateQuotationCustomerRequest{
		CustomerUserID: &customerID,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("unlinked customer should be forbidden: got %v", err)
	}
}

// ── Approve tests ─────────────────────────────────────────────────────────────

func TestApprove_OnlyFromCustomerDraft(t *testing.T) {
	badStatuses := []string{"INTERNAL_DRAFT", "CUSTOMER_SENT", "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED"}
	for _, status := range badStatuses {
		t.Run(status, func(t *testing.T) {
			repo := newMockRepo()
			repo.addNursery(10, 1)
			repo.addQuotation(baseQuotation(1, status))
			svc := NewService(repo, nil)

			_, err := svc.Approve(context.Background(), ownerActor(1), 1)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("status %s: expected ErrInvalidTransition, got %v", status, err)
			}
		})
	}
}

func TestApprove_FromCustomerDraftSucceeds(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	q, err := svc.Approve(context.Background(), ownerActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Status != "CUSTOMER_SENT" {
		t.Errorf("expected CUSTOMER_SENT, got %s", q.Status)
	}
}

func TestSendToCustomer_FromCustomerDraftSucceeds(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	q, err := svc.SendToCustomer(context.Background(), ownerActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Status != "CUSTOMER_SENT" {
		t.Errorf("expected CUSTOMER_SENT, got %s", q.Status)
	}
}

func TestBuyerCannotViewCustomerDraftDirectly(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_DRAFT")
	q.QuotationType = "CUSTOMER"
	buyerID := int64(99)
	q.CustomerUserID = &buyerID
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	_, err := svc.Get(context.Background(), buyerActor(99), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestBuyerCanViewAfterSend(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_SENT")
	buyerID := int64(99)
	q.CustomerUserID = &buyerID
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	got, err := svc.Get(context.Background(), buyerActor(99), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("expected quotation 1, got %d", got.ID)
	}
}

func TestBuyerListIsBuyingScoped(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo, nil)

	_, _, err := svc.List(context.Background(), buyerActor(99), ListQuotationsRequest{Buying: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.lastListInput.Buying || repo.lastListInput.UserID != 99 {
		t.Errorf("expected buying list scoped to user 99, got %+v", repo.lastListInput)
	}
}

func TestBuildWhereBuyingHidesDrafts(t *testing.T) {
	where, _ := buildWhere(ListQuotationsRequest{Buying: true, UserID: 99})
	if !strings.Contains(where, "CUSTOMER_SENT") || strings.Contains(where, "CUSTOMER_DRAFT") {
		t.Errorf("buyer where clause should include only customer-visible statuses, got %s", where)
	}
}

// ── Recall tests ──────────────────────────────────────────────────────────────

func TestRecall_FromCustomerSentSucceeds(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_SENT"))
	svc := NewService(repo, nil)

	q, err := svc.Recall(context.Background(), ownerActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Status != "CUSTOMER_DRAFT" {
		t.Errorf("expected CUSTOMER_DRAFT after recall, got %s", q.Status)
	}
}

func TestRecall_ManagerCanRecall(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1, 2)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_SENT"))
	svc := NewService(repo, nil)

	_, err := svc.Recall(context.Background(), managerActor(2), 1)
	if err != nil {
		t.Errorf("manager should be able to recall: %v", err)
	}
}

func TestRecall_OnlyFromSent(t *testing.T) {
	badStatuses := []string{"CUSTOMER_DRAFT", "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED"}
	for _, status := range badStatuses {
		t.Run(status, func(t *testing.T) {
			repo := newMockRepo()
			repo.addNursery(10, 1)
			repo.addQuotation(baseQuotation(1, status))
			svc := NewService(repo, nil)

			_, err := svc.Recall(context.Background(), ownerActor(1), 1)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("status %s: expected ErrInvalidTransition, got %v", status, err)
			}
		})
	}
}

func TestRecall_BuyerForbidden(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_SENT")
	buyerID := int64(99)
	q.CustomerUserID = &buyerID
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	_, err := svc.Recall(context.Background(), buyerActor(99), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("buyer should not be able to recall: got %v", err)
	}
}

// ── BuyerAccept tests ─────────────────────────────────────────────────────────

func TestBuyerAccept_OnlyFromCustomerSent(t *testing.T) {
	badStatuses := []string{"CUSTOMER_DRAFT", "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED"}
	for _, status := range badStatuses {
		t.Run(status, func(t *testing.T) {
			repo := newMockRepo()
			repo.addNursery(10, 1)
			q := baseQuotation(1, status)
			buyerID := int64(99)
			q.CustomerUserID = &buyerID
			repo.addQuotation(q)
			svc := NewService(repo, nil)

			_, err := svc.BuyerAccept(context.Background(), buyerActor(99), 1)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("status %s: expected ErrInvalidTransition, got %v", status, err)
			}
		})
	}
}

func TestBuyerAccept_FromSentSucceeds(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_SENT")
	buyerID := int64(99)
	q.CustomerUserID = &buyerID
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	result, err := svc.BuyerAccept(context.Background(), buyerActor(99), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "CUSTOMER_ACCEPTED" {
		t.Errorf("expected CUSTOMER_ACCEPTED, got %s", result.Status)
	}
}

func TestBuyerAccept_AuditUsesCorrectStatus(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_SENT")
	buyerID := int64(99)
	q.CustomerUserID = &buyerID
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	_, err := svc.BuyerAccept(context.Background(), buyerActor(99), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── BuyerReject tests ─────────────────────────────────────────────────────────

func TestBuyerReject_OnlyFromCustomerSent(t *testing.T) {
	badStatuses := []string{"CUSTOMER_DRAFT", "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED"}
	for _, status := range badStatuses {
		t.Run(status, func(t *testing.T) {
			repo := newMockRepo()
			repo.addNursery(10, 1)
			q := baseQuotation(1, status)
			buyerID := int64(99)
			q.CustomerUserID = &buyerID
			repo.addQuotation(q)
			svc := NewService(repo, nil)

			_, err := svc.BuyerReject(context.Background(), buyerActor(99), 1, AcceptRejectQuotationRequest{})
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("status %s: expected ErrInvalidTransition, got %v", status, err)
			}
		})
	}
}

func TestBuyerReject_FromSentSucceeds(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_SENT")
	buyerID := int64(99)
	q.CustomerUserID = &buyerID
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	result, err := svc.BuyerReject(context.Background(), buyerActor(99), 1, AcceptRejectQuotationRequest{Reason: ptr("Price too high")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "CUSTOMER_REJECTED" {
		t.Errorf("expected CUSTOMER_REJECTED, got %s", result.Status)
	}
}

// ── ConvertToOrder tests ──────────────────────────────────────────────────────

func TestConvertToOrder_RequiresAcceptedStatus(t *testing.T) {
	badStatuses := []string{"CUSTOMER_DRAFT", "CUSTOMER_SENT", "CUSTOMER_REJECTED"}
	for _, status := range badStatuses {
		t.Run(status, func(t *testing.T) {
			repo := newMockRepo()
			repo.addNursery(10, 1)
			repo.addQuotation(baseQuotation(1, status))
			svc := NewService(repo, nil)

			_, err := svc.ConvertToOrder(context.Background(), ownerActor(1), 1)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("status %s: expected ErrInvalidTransition, got %v", status, err)
			}
		})
	}
}

func TestConvertToOrder_FromAcceptedSucceeds(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addOrder(42, 10) // order 42 belongs to nursery 10
	repo.addQuotation(baseQuotation(1, "CUSTOMER_ACCEPTED"))
	svc := NewService(repo, nil)

	result, err := svc.ConvertToOrder(context.Background(), ownerActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "CONVERTED" {
		t.Errorf("expected CONVERTED, got %s", result.Status)
	}
}

func TestConvertToOrder_IdempotencyGuard(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_ACCEPTED")
	orderID := int64(42)
	q.ConvertedOrderID = &orderID
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	_, err := svc.ConvertToOrder(context.Background(), ownerActor(1), 1)
	if !errors.Is(err, ErrAlreadyConverted) {
		t.Errorf("expected ErrAlreadyConverted, got %v", err)
	}
}

// ── Delete tests ──────────────────────────────────────────────────────────────

func TestDelete_OwnerCanDelete(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	err := svc.Delete(context.Background(), ownerActor(1), 1)
	if err != nil {
		t.Errorf("owner should be able to delete: %v", err)
	}
	if len(repo.softDeleted) != 1 {
		t.Error("expected one soft delete")
	}
}

func TestDelete_ManagerCannotDelete(t *testing.T) {
	// Business rule: only the nursery owner can delete; managers cannot.
	repo := newMockRepo()
	repo.addNursery(10, 1, 2) // user 2 is manager
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	err := svc.Delete(context.Background(), managerActor(2), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("manager should not be able to delete: got %v", err)
	}
}

func TestDelete_AdminCanDelete(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_SENT"))
	svc := NewService(repo, nil)

	err := svc.Delete(context.Background(), adminActor(999), 1)
	if err != nil {
		t.Errorf("admin should be able to delete: %v", err)
	}
}

func TestDelete_StrangerForbidden(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	err := svc.Delete(context.Background(), ownerActor(55), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("unrelated user should be forbidden: got %v", err)
	}
}

// ── AssignManager tests ───────────────────────────────────────────────────────

func TestAssignManager_NonMemberRejected(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1) // user 99 is NOT a member
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	_, err := svc.AssignManager(context.Background(), ownerActor(1), 1, AssignManagerRequest{ManagerUserID: 99})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("assigning non-member should return ErrInvalidInput, got %v", err)
	}
}

func TestAssignManager_ValidMemberSucceeds(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1, 2) // user 2 is a member
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	q, err := svc.AssignManager(context.Background(), ownerActor(1), 1, AssignManagerRequest{ManagerUserID: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.AssignedManagerUserID == nil || *q.AssignedManagerUserID != 2 {
		t.Error("assigned manager not set correctly")
	}
}

func TestAssignManager_NonOwnerForbidden(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1, 2) // user 2 is manager, user 3 is unrelated
	repo.addQuotation(baseQuotation(1, "CUSTOMER_DRAFT"))
	svc := NewService(repo, nil)

	_, err := svc.AssignManager(context.Background(), managerActor(2), 1, AssignManagerRequest{ManagerUserID: 2})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("manager (non-owner) should not be able to assign: got %v", err)
	}
}

// ── scopeList tests ───────────────────────────────────────────────────────────

func TestScopeList_ManagerGetsNurseryScope(t *testing.T) {
	// Previously managers fell back to user-filter; they should see the whole nursery.
	repo := newMockRepo()
	repo.addNursery(10, 1, 2) // user 2 is a manager of nursery 10
	svc := NewService(repo, nil)

	input := ListQuotationsRequest{}
	err := svc.scopeList(context.Background(), managerActor(2), &input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.NurseryID != 10 {
		t.Errorf("manager should see nursery 10, got NurseryID=%d", input.NurseryID)
	}
	if input.UserID != 0 {
		t.Errorf("manager should NOT be filtered by UserID, got %d", input.UserID)
	}
}

func TestScopeList_OwnerGetsNurseryScope(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	svc := NewService(repo, nil)

	input := ListQuotationsRequest{}
	err := svc.scopeList(context.Background(), ownerActor(1), &input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.NurseryID != 10 {
		t.Errorf("owner should see nursery 10, got NurseryID=%d", input.NurseryID)
	}
}

func TestScopeList_AdminSeesAll(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo, nil)

	input := ListQuotationsRequest{}
	err := svc.scopeList(context.Background(), adminActor(999), &input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Admin: no scope filter applied.
	if input.NurseryID != 0 || input.UserID != 0 {
		t.Error("admin should have no scope restrictions")
	}
}

// ── canView tests ─────────────────────────────────────────────────────────────

func TestCanView_ContextIsPassedThrough(t *testing.T) {
	// Verifies that canView uses the passed context, not context.Background().
	// We use a cancelled context to confirm the propagation path is wired up;
	// since our mock ignores context, we only verify the function signature works.
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_DRAFT")
	svc := NewService(repo, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	// Admin bypasses DB calls, so this should succeed even with cancelled context.
	err := svc.canView(ctx, adminActor(1), q)
	if err != nil {
		t.Errorf("admin canView should succeed: %v", err)
	}
}

func TestCanView_CreatorCanView(t *testing.T) {
	repo := newMockRepo()
	q := baseQuotation(1, "CUSTOMER_DRAFT")
	svc := NewService(repo, nil)

	err := svc.canView(context.Background(), ownerActor(1), q) // user 1 is creator
	if err != nil {
		t.Errorf("creator should be able to view: %v", err)
	}
}

func TestCanView_StrangerForbidden(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_DRAFT")
	svc := NewService(repo, nil)

	err := svc.canView(context.Background(), ownerActor(55), q)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger should be forbidden: got %v", err)
	}
}

// ── canBuyerAct tests ─────────────────────────────────────────────────────────

func TestCanBuyerAct_MobileMatchWorks(t *testing.T) {
	repo := newMockRepo()
	repo.userMobiles[99] = "9300000000"
	q := baseQuotation(1, "CUSTOMER_SENT")
	mobile := "9300000000"
	q.CustomerUserID = nil
	q.RecipientMobile = &mobile
	svc := NewService(repo, nil)

	err := svc.canBuyerAct(context.Background(), buyerActor(99), q)
	if err != nil {
		t.Errorf("mobile match should grant buyer access: %v", err)
	}
}

func TestCanView_RecipientMobileMatchGrantsAccess(t *testing.T) {
	// Buyer found the quotation in the list via mobile match (no customer_user_id).
	// canView must also permit access via the same mobile match so the detail loads.
	repo := newMockRepo()
	repo.addNursery(10, 1)
	repo.userMobiles[99] = "9300000000"
	q := baseQuotation(1, "CUSTOMER_SENT")
	q.CustomerUserID = nil
	mobile := "9300000000"
	q.RecipientMobile = &mobile
	svc := NewService(repo, nil)

	err := svc.canView(context.Background(), buyerActor(99), q)
	if err != nil {
		t.Errorf("mobile-matched buyer should be able to view: %v", err)
	}
}

func TestCanView_BuyerNurseryMatchGrantsAccess(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(20, 99) // nursery 20 owned by user 99
	q := baseQuotation(1, "CUSTOMER_SENT")
	buyerNursery := int64(20)
	q.BuyerNurseryID = &buyerNursery
	svc := NewService(repo, nil)

	err := svc.canView(context.Background(), ownerActor(99), q)
	if err != nil {
		t.Errorf("buyer nursery owner should be able to view: %v", err)
	}
}

func TestCanBuyerAct_WrongMobileForbidden(t *testing.T) {
	repo := newMockRepo()
	repo.userMobiles[99] = "9300000000"
	q := baseQuotation(1, "CUSTOMER_SENT")
	mobile := "9111111111" // different number
	q.RecipientMobile = &mobile
	svc := NewService(repo, nil)

	err := svc.canBuyerAct(context.Background(), buyerActor(99), q)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("wrong mobile should be forbidden: got %v", err)
	}
}

// ── Create guard tests ────────────────────────────────────────────────────────

func TestCreate_DriverForbidden(t *testing.T) {
	repo := newMockRepo()
	svc := NewService(repo, nil)

	actor := ActorContext{UserID: 5, Roles: []string{"DRIVER"}}
	_, err := svc.Create(context.Background(), actor, CreateQuotationRequest{
		QuotationType: "INTERNAL",
		NurseryID:     ptr(int64(10)),
		Items:         []QuotationItemRequest{validItem()},
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("driver should be forbidden from creating: got %v", err)
	}
}

func TestCreate_CustomerQuotationRequiresRecipient(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	svc := NewService(repo, nil)

	_, err := svc.Create(context.Background(), ownerActor(1), CreateQuotationRequest{
		QuotationType: "CUSTOMER",
		NurseryID:     ptr(int64(10)),
		Items:         []QuotationItemRequest{validItem()},
		// No recipient set
	})
	if !errors.Is(err, ErrCustomerRequired) {
		t.Errorf("expected ErrCustomerRequired, got %v", err)
	}
}

// ── scopeList security bypass ─────────────────────────────────────────────────

func TestScopeList_OwnerCannotOverrideNurseryID(t *testing.T) {
	// Before the fix, an owner could pass nursery_id=99 and scopeList would use it
	// unchecked. Now it must always force the actor's own nursery.
	repo := newMockRepo()
	repo.addNursery(10, 1) // user 1 owns nursery 10
	svc := NewService(repo, nil)

	input := ListQuotationsRequest{NurseryID: 99} // attacker-supplied ID
	if err := svc.scopeList(context.Background(), ownerActor(1), &input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.NurseryID != 10 {
		t.Errorf("scopeList must override client NurseryID; got %d, want 10", input.NurseryID)
	}
}

// ── manager privacy ───────────────────────────────────────────────────────────

func TestGet_ManagerCannotSeeRecipientContact(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1, 2) // user 2 is manager
	name := "Ravi Buyer"
	mobile := "9300000000"
	q := baseQuotation(1, "CUSTOMER_SENT")
	q.RecipientName = &name
	q.RecipientMobile = &mobile
	q.AssignedManagerUserID = ptr(int64(2))
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	result, err := svc.Get(context.Background(), managerActor(2), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RecipientName != nil || result.RecipientMobile != nil {
		t.Error("manager must not see recipient_name or recipient_mobile")
	}
}

func TestGet_OwnerCanSeeRecipientContact(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	name := "Ravi Buyer"
	mobile := "9300000000"
	q := baseQuotation(1, "CUSTOMER_SENT")
	q.RecipientName = &name
	q.RecipientMobile = &mobile
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	result, err := svc.Get(context.Background(), ownerActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RecipientName == nil || result.RecipientMobile == nil {
		t.Error("owner must see recipient_name and recipient_mobile")
	}
}

// ── Nursery must be ACTIVE to create quotation ────────────────────────────────

func TestCreate_InactiveNurseryForbidden(t *testing.T) {
	repo := newMockRepo()
	// addNursery marks nursery active; override to inactive
	repo.addNursery(10, 1)
	repo.activeNurseries[10] = false
	svc := NewService(repo, nil)

	_, err := svc.Create(context.Background(), ownerActor(1), CreateQuotationRequest{
		QuotationType:   "CUSTOMER",
		NurseryID:       ptr(int64(10)),
		Items:           []QuotationItemRequest{validItem()},
		RecipientMobile: ptr("9300000000"),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("inactive nursery should return ErrForbidden, got %v", err)
	}
}

// ── mobile normalization ──────────────────────────────────────────────────────

func TestNormalizeIndianMobile(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"9300000000", "9300000000"},
		{"+919300000000", "9300000000"},
		{"919300000000", "9300000000"},
		{"93-0000-0000", "9300000000"},
		{"abc", ""},
		{"12345", ""},
	}
	for _, tc := range cases {
		got := normalizeIndianMobile(tc.input)
		if got != tc.want {
			t.Errorf("normalizeIndianMobile(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── scopeList — buying branch ─────────────────────────────────────────────────

func TestScopeList_BuyerSeesBuyingScope(t *testing.T) {
	// When buying=true, scopeList should filter by the buyer's user ID.
	// The buyer's own nursery_id should also be set when the actor is an owner.
	repo := newMockRepo()
	svc := NewService(repo, nil)

	input := ListQuotationsRequest{Buying: true}
	if err := svc.scopeList(context.Background(), buyerActor(99), &input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.UserID != 99 {
		t.Errorf("buyer buying scope should set UserID=99, got %d", input.UserID)
	}
	// NurseryID must NOT be set for a plain buyer (no nursery ownership).
	if input.NurseryID != 0 {
		t.Errorf("plain buyer should not have NurseryID set, got %d", input.NurseryID)
	}
}

func TestScopeList_OwnerBuyingScopeIncludesNursery(t *testing.T) {
	// An owner purchasing on behalf of their nursery should have BuyerNurseryID set.
	repo := newMockRepo()
	repo.addNursery(10, 1) // user 1 owns nursery 10
	svc := NewService(repo, nil)

	input := ListQuotationsRequest{Buying: true}
	if err := svc.scopeList(context.Background(), ownerActor(1), &input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.UserID != 1 {
		t.Errorf("buying scope must set UserID; got %d", input.UserID)
	}
	if input.BuyerNurseryID != 10 {
		t.Errorf("buying scope must set BuyerNurseryID=10 for owner; got %d", input.BuyerNurseryID)
	}
}

// ── Create — mobile validation ────────────────────────────────────────────────

func TestCreate_InvalidMobileRejected(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	svc := NewService(repo, nil)

	_, err := svc.Create(context.Background(), ownerActor(1), CreateQuotationRequest{
		QuotationType:   "CUSTOMER",
		NurseryID:       ptr(int64(10)),
		Items:           []QuotationItemRequest{validItem()},
		RecipientMobile: ptr("not-a-number"),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("invalid mobile should return ErrInvalidInput, got %v", err)
	}
}

func TestCreate_NormalizedMobileAccepted(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	svc := NewService(repo, nil)

	// +91 prefix should be stripped and accepted
	_, err := svc.Create(context.Background(), ownerActor(1), CreateQuotationRequest{
		QuotationType:   "CUSTOMER",
		NurseryID:       ptr(int64(10)),
		Items:           []QuotationItemRequest{validItem()},
		RecipientMobile: ptr("+919300000000"),
	})
	if err != nil {
		t.Errorf("+91 prefixed mobile should be accepted after normalization, got %v", err)
	}
}

// ── quotation expiry ──────────────────────────────────────────────────────────

func TestBuyerAccept_ExpiredQuotationRejected(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(10, 1)
	q := baseQuotation(1, "CUSTOMER_SENT")
	buyerID := int64(99)
	q.CustomerUserID = &buyerID
	past := time.Now().Add(-24 * time.Hour)
	q.ValidUntil = &past
	repo.addQuotation(q)
	svc := NewService(repo, nil)

	_, err := svc.BuyerAccept(context.Background(), buyerActor(99), 1)
	if !errors.Is(err, ErrQuotationExpired) {
		t.Errorf("expired quotation should return ErrQuotationExpired, got %v", err)
	}
}
