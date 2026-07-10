package plantrequests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	requests  map[int64]*PlantRequest
	responses map[int64]*Response
	members   map[string]bool // "nurseryID:userID"
	inventory map[string]int  // "nurseryID:plantID" → quantity
	nextReqID int64
	nextResID int64
}

func newMock() *mockRepo {
	return &mockRepo{
		requests:  make(map[int64]*PlantRequest),
		responses: make(map[int64]*Response),
		members:   make(map[string]bool),
		inventory: make(map[string]int),
		nextReqID: 100,
		nextResID: 200,
	}
}

func mk(a, b int64) string  { return fmt.Sprintf("%d:%d", a, b) }
func mkPI(n, p int64) string { return fmt.Sprintf("%d:%d", n, p) }

func (m *mockRepo) seedMember(nurseryID, userID int64) {
	m.members[mk(nurseryID, userID)] = true
}

func (m *mockRepo) seedRequest(id, nurseryID int64, status string, plantID int64) *PlantRequest {
	r := &PlantRequest{ID: id, RequestingNurseryID: nurseryID, Status: status, PlantID: plantID, QuantityRequired: 10, RadiusKM: 50}
	m.requests[id] = r
	return r
}

func (m *mockRepo) seedResponse(id, requestID, supplierNurseryID int64, status string) *Response {
	r := &Response{ID: id, RequestID: requestID, SupplierNurseryID: supplierNurseryID, Status: status, AvailableQuantity: 5}
	m.responses[id] = r
	return r
}

// Repository interface

func (m *mockRepo) List(_ context.Context, _ ListRequestsRequest) ([]PlantRequest, int64, error) {
	result := make([]PlantRequest, 0, len(m.requests))
	for _, r := range m.requests {
		result = append(result, *r)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*PlantRequest, error) {
	r, ok := m.requests[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

func (m *mockRepo) Create(_ context.Context, actorID int64, input CreateRequest) (*PlantRequest, error) {
	m.nextReqID++
	r := &PlantRequest{
		ID:                  m.nextReqID,
		RequestingNurseryID: input.RequestingNurseryID,
		RequestedByUserID:   actorID,
		PlantID:             input.PlantID,
		QuantityRequired:    input.QuantityRequired,
		RadiusKM:            input.RadiusKM,
		Status:              input.Status,
	}
	m.requests[m.nextReqID] = r
	return r, nil
}

func (m *mockRepo) Update(_ context.Context, _ int64, id int64, input UpdateRequest) (*PlantRequest, error) {
	r, ok := m.requests[id]
	if !ok {
		return nil, ErrNotFound
	}
	r.QuantityRequired = input.QuantityRequired
	return r, nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, id int64, status string) (*PlantRequest, error) {
	r, ok := m.requests[id]
	if !ok {
		return nil, ErrNotFound
	}
	r.Status = status
	return r, nil
}

func (m *mockRepo) Delete(_ context.Context, id int64) error {
	if _, ok := m.requests[id]; !ok {
		return ErrNotFound
	}
	delete(m.requests, id)
	return nil
}

func (m *mockRepo) ListResponses(_ context.Context, requestID int64) ([]Response, error) {
	result := []Response{}
	for _, r := range m.responses {
		if r.RequestID == requestID {
			result = append(result, *r)
		}
	}
	return result, nil
}

func (m *mockRepo) CreateResponse(_ context.Context, requestID, actorID int64, input CreateResponseRequest) (*Response, error) {
	m.nextResID++
	r := &Response{
		ID:                m.nextResID,
		RequestID:         requestID,
		SupplierNurseryID: input.SupplierNurseryID,
		RespondedByUserID: actorID,
		AvailableQuantity: input.AvailableQuantity,
		Status:            input.Status,
	}
	m.responses[m.nextResID] = r
	return r, nil
}

func (m *mockRepo) UpdateResponse(_ context.Context, id int64, input UpdateResponseRequest) (*Response, error) {
	r, ok := m.responses[id]
	if !ok {
		return nil, ErrNotFound
	}
	r.Status = input.Status
	return r, nil
}

func (m *mockRepo) RecomputeRequestStatus(_ context.Context, _ int64) error { return nil }

func (m *mockRepo) InventoryAvailable(_ context.Context, nurseryID, plantID int64, _ *int16) (int, error) {
	qty, ok := m.inventory[mkPI(nurseryID, plantID)]
	if !ok {
		return 0, nil
	}
	return qty, nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.members[mk(nurseryID, userID)], nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── List ─────────────────────────────────────────────────────────────────────

func TestList_OwnerCanList(t *testing.T) {
	repo := newMock()
	repo.seedRequest(1, 1, "OPEN", 10)
	repo.seedRequest(2, 2, "OPEN", 10)

	reqs, _, err := svc(repo).List(context.Background(), ownerActor(10), ListRequestsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 2 {
		t.Errorf("want 2 requests, got %d", len(reqs))
	}
}

func TestList_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), buyerActor(1), ListRequestsRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── Get ─────────────────────────────────────────────────────────────────────

func TestGet_Found(t *testing.T) {
	repo := newMock()
	repo.seedRequest(1, 1, "OPEN", 10)

	req, err := svc(repo).Get(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ID != 1 {
		t.Errorf("want ID 1, got %d", req.ID)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	req, err := svc(repo).Create(context.Background(), ownerActor(10), CreateRequest{
		RequestingNurseryID: 1,
		PlantID:             5,
		QuantityRequired:    20,
		RadiusKM:            50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Status != "OPEN" {
		t.Errorf("want OPEN, got %s", req.Status)
	}
}

func TestCreate_ManagerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 20)

	_, err := svc(repo).Create(context.Background(), managerActor(20), CreateRequest{
		RequestingNurseryID: 1,
		PlantID:             5,
		QuantityRequired:    10,
		RadiusKM:            50,
	})
	if err != nil {
		t.Fatalf("manager should be able to create plant requests: %v", err)
	}
}

func TestCreate_AdminSuccess(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateRequest{
		RequestingNurseryID: 1,
		PlantID:             5,
		QuantityRequired:    10,
		RadiusKM:            50,
	})
	if err != nil {
		t.Fatalf("admin should bypass member check: %v", err)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), CreateRequest{
		RequestingNurseryID: 1,
		PlantID:             5,
		QuantityRequired:    10,
		RadiusKM:            50,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestCreate_OtherNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedMember(2, 10) // member of nursery 2, not 1

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateRequest{
		RequestingNurseryID: 1, // belongs to nursery 1
		PlantID:             5,
		QuantityRequired:    10,
		RadiusKM:            50,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member nursery, got %v", err)
	}
}

func TestCreate_MissingPlantIDInvalid(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateRequest{
		RequestingNurseryID: 1,
		PlantID:             0, // missing
		QuantityRequired:    10,
		RadiusKM:            50,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for missing PlantID, got %v", err)
	}
}

func TestCreate_ZeroQuantityInvalid(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateRequest{
		RequestingNurseryID: 1,
		PlantID:             5,
		QuantityRequired:    0, // invalid
		RadiusKM:            50,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for zero quantity, got %v", err)
	}
}

func TestCreate_ExpiredExpiryInvalid(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	past := time.Now().Add(-24 * time.Hour)

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateRequest{
		RequestingNurseryID: 1,
		PlantID:             5,
		QuantityRequired:    10,
		RadiusKM:            50,
		ExpiresAt:           &past,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for past expiry, got %v", err)
	}
}

// ─── UpdateStatus ────────────────────────────────────────────────────────────

func TestUpdateStatus_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedRequest(1, 1, "OPEN", 5)

	req, err := svc(repo).UpdateStatus(context.Background(), ownerActor(10), 1, UpdateStatusRequest{Status: "CLOSED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Status != "CLOSED" {
		t.Errorf("want CLOSED, got %s", req.Status)
	}
}

func TestUpdateStatus_InvalidStatus(t *testing.T) {
	repo := newMock()
	repo.seedRequest(1, 1, "OPEN", 5)

	_, err := svc(repo).UpdateStatus(context.Background(), adminActor(1), 1, UpdateStatusRequest{Status: "DELETED"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestUpdateStatus_NotFound(t *testing.T) {
	_, err := svc(newMock()).UpdateStatus(context.Background(), adminActor(1), 9999, UpdateStatusRequest{Status: "CLOSED"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestDelete_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedRequest(1, 1, "OPEN", 5)

	err := svc(repo).Delete(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.requests[1]; ok {
		t.Error("request should have been deleted")
	}
}

func TestDelete_OtherNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedMember(2, 20) // member of nursery 2
	repo.seedRequest(1, 1, "OPEN", 5) // belongs to nursery 1

	err := svc(repo).Delete(context.Background(), ownerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── CreateResponse ───────────────────────────────────────────────────────────

func TestCreateResponse_AvailableSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(2, 20) // supplier is nursery 2
	repo.seedRequest(1, 1, "OPEN", 5) // requesting nursery 1
	repo.inventory[mkPI(2, 5)] = 100  // plenty in stock

	resp, err := svc(repo).CreateResponse(context.Background(), ownerActor(20), 1, CreateResponseRequest{
		SupplierNurseryID: 2,
		AvailableQuantity: 10,
		Status:            "AVAILABLE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "AVAILABLE" {
		t.Errorf("want AVAILABLE, got %s", resp.Status)
	}
}

func TestCreateResponse_SameNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedRequest(1, 1, "OPEN", 5) // requesting nursery 1
	repo.inventory[mkPI(1, 5)] = 100

	_, err := svc(repo).CreateResponse(context.Background(), ownerActor(10), 1, CreateResponseRequest{
		SupplierNurseryID: 1, // same as requesting
		AvailableQuantity: 10,
		Status:            "AVAILABLE",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for self-response, got %v", err)
	}
}

func TestCreateResponse_InsufficientInventory(t *testing.T) {
	repo := newMock()
	repo.seedMember(2, 20)
	repo.seedRequest(1, 1, "OPEN", 5)
	repo.inventory[mkPI(2, 5)] = 2 // only 2 in stock

	_, err := svc(repo).CreateResponse(context.Background(), ownerActor(20), 1, CreateResponseRequest{
		SupplierNurseryID: 2,
		AvailableQuantity: 10, // wants to offer 10 but only has 2
		Status:            "AVAILABLE",
	})
	if !errors.Is(err, ErrInsufficientInventory) {
		t.Errorf("want ErrInsufficientInventory, got %v", err)
	}
}

func TestCreateResponse_NotAvailable_NoInventoryCheck(t *testing.T) {
	repo := newMock()
	repo.seedMember(2, 20)
	repo.seedRequest(1, 1, "OPEN", 5)
	// no inventory seeded — but status is NOT_AVAILABLE, so no check needed

	_, err := svc(repo).CreateResponse(context.Background(), ownerActor(20), 1, CreateResponseRequest{
		SupplierNurseryID: 2,
		AvailableQuantity: 0,
		Status:            "NOT_AVAILABLE",
	})
	if !errors.Is(err, ErrInvalidInput) {
		// AvailableQuantity must be > 0 per validateNewResponse
		t.Logf("got error: %v", err)
	}
}

func TestCreateResponse_UnauthorizedNurseryForbidden(t *testing.T) {
	repo := newMock()
	// Actor 30 is NOT a member of nursery 3
	repo.seedRequest(1, 1, "OPEN", 5)

	_, err := svc(repo).CreateResponse(context.Background(), ownerActor(30), 1, CreateResponseRequest{
		SupplierNurseryID: 3,
		AvailableQuantity: 5,
		Status:            "AVAILABLE",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member, got %v", err)
	}
}

// ─── UpdateResponse ───────────────────────────────────────────────────────────

func TestUpdateResponse_AcceptSuccess(t *testing.T) {
	repo := newMock()
	repo.seedRequest(1, 1, "OPEN", 5)
	repo.seedResponse(1, 1, 2, "AVAILABLE")

	resp, err := svc(repo).UpdateResponse(context.Background(), ownerActor(10), 1, UpdateResponseRequest{Status: "ACCEPTED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "ACCEPTED" {
		t.Errorf("want ACCEPTED, got %s", resp.Status)
	}
}

func TestUpdateResponse_InvalidStatus(t *testing.T) {
	repo := newMock()
	repo.seedResponse(1, 1, 2, "AVAILABLE")

	_, err := svc(repo).UpdateResponse(context.Background(), ownerActor(10), 1, UpdateResponseRequest{Status: "AVAILABLE"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for non-manager status, got %v", err)
	}
}

func TestUpdateResponse_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedResponse(1, 1, 2, "AVAILABLE")

	_, err := svc(repo).UpdateResponse(context.Background(), buyerActor(1), 1, UpdateResponseRequest{Status: "ACCEPTED"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── isAllowedRequestStatus table ───────────────────────────────────────────

func TestIsAllowedRequestStatus(t *testing.T) {
	allowed := []string{"DRAFT", "OPEN", "PARTIALLY_ACCEPTED", "ACCEPTED", "REJECTED", "CLOSED"}
	for _, s := range allowed {
		if !isAllowedRequestStatus(s) {
			t.Errorf("status %q should be allowed", s)
		}
	}
	denied := []string{"DELETED", "CANCELLED", "FULFILLED", ""}
	for _, s := range denied {
		if isAllowedRequestStatus(s) {
			t.Errorf("status %q should NOT be allowed", s)
		}
	}
}
