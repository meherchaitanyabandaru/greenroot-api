package dispatches

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	dispatches     map[int64]*Dispatch
	items          map[int64]*DispatchItem
	tripEvents     []TripEvent
	orderAccess    map[int64]*OrderAccess // order_id → access
	nurseryMembers map[int64][]int64      // nursery_id → []user_id
	ownedNurseries map[int64]int64        // user_id → nursery_id
	userNurseries  map[int64][]int64      // user_id → []nursery_id
	driverUserIDs  map[int64]int64        // driver_id → user_id
	duplicates     map[string]bool
	nextID         int64
	nextItemID     int64
	nextEventID    int64
}

func newMock() *mockRepo {
	return &mockRepo{
		dispatches:     make(map[int64]*Dispatch),
		items:          make(map[int64]*DispatchItem),
		orderAccess:    make(map[int64]*OrderAccess),
		nurseryMembers: make(map[int64][]int64),
		ownedNurseries: make(map[int64]int64),
		userNurseries:  make(map[int64][]int64),
		driverUserIDs:  make(map[int64]int64),
		duplicates:     make(map[string]bool),
	}
}

func (m *mockRepo) seedNursery(nurseryID, ownerID int64, members ...int64) {
	m.ownedNurseries[ownerID] = nurseryID
	all := append([]int64{ownerID}, members...)
	m.nurseryMembers[nurseryID] = all
	for _, uid := range all {
		m.userNurseries[uid] = append(m.userNurseries[uid], nurseryID)
	}
}

func (m *mockRepo) seedDispatch(d Dispatch) {
	m.dispatches[d.ID] = &d
}

func (m *mockRepo) seedOrderAccess(orderID int64, nurseryID *int64) {
	m.orderAccess[orderID] = &OrderAccess{OrderID: orderID, NurseryID: nurseryID}
}

// Repository interface implementation

func (m *mockRepo) List(_ context.Context, _ ListDispatchesRequest) ([]Dispatch, int64, error) {
	result := make([]Dispatch, 0, len(m.dispatches))
	for _, d := range m.dispatches {
		result = append(result, *d)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*Dispatch, error) {
	d, ok := m.dispatches[id]
	if !ok {
		return nil, ErrNotFound
	}
	return d, nil
}

func (m *mockRepo) FindByCode(_ context.Context, code string) (*Dispatch, error) {
	for _, d := range m.dispatches {
		if d.DispatchCode == code {
			return d, nil
		}
	}
	return nil, ErrNotFound
}

func (m *mockRepo) FindByTrackingUUID(_ context.Context, uuid string) (*Dispatch, error) {
	for _, d := range m.dispatches {
		if d.TrackingUUID != nil && *d.TrackingUUID == uuid {
			return d, nil
		}
	}
	return nil, ErrNotFound
}

func (m *mockRepo) HasDuplicate(_ context.Context, num string) (bool, error) {
	return m.duplicates[num], nil
}

func (m *mockRepo) Create(_ context.Context, actorID int64, input CreateDispatchInput) (*Dispatch, error) {
	m.nextID++
	d := &Dispatch{
		ID:           m.nextID,
		DispatchCode: "DC-TEST-001",
		OrderID:      input.OrderID,
		Status:       "PENDING",
		DispatchedBy: &actorID,
	}
	if input.DispatchNumber != nil {
		d.DispatchNumber = input.DispatchNumber
	}
	m.dispatches[d.ID] = d
	return d, nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, dispatchID int64, input UpdateStatusInput) (*Dispatch, error) {
	d, ok := m.dispatches[dispatchID]
	if !ok {
		return nil, ErrNotFound
	}
	d.Status = input.Status
	return d, nil
}

func (m *mockRepo) SetDriverUser(_ context.Context, dispatchID int64, userID int64) (*Dispatch, error) {
	d, ok := m.dispatches[dispatchID]
	if !ok {
		return nil, ErrNotFound
	}
	d.DriverUserID = &userID
	d.Status = "ACCEPTED"
	return d, nil
}

func (m *mockRepo) CreateNotification(_ context.Context, _ int64, _, _, _ string) error {
	return nil
}

func (m *mockRepo) AcknowledgeDeliveryUpdate(_ context.Context, dispatchID int64, _ int64) error {
	if _, ok := m.dispatches[dispatchID]; !ok {
		return ErrNotFound
	}
	return nil
}

func (m *mockRepo) CreateItem(_ context.Context, dispatchID int64, input DispatchItemRequest) (*DispatchItem, error) {
	m.nextItemID++
	item := &DispatchItem{
		ID:          m.nextItemID,
		DispatchID:  dispatchID,
		OrderItemID: input.OrderItemID,
		Quantity:    input.Quantity,
	}
	m.items[item.ID] = item
	return item, nil
}

func (m *mockRepo) ListItems(_ context.Context, dispatchID int64) ([]DispatchItem, error) {
	var result []DispatchItem
	for _, item := range m.items {
		if item.DispatchID == dispatchID {
			result = append(result, *item)
		}
	}
	return result, nil
}

func (m *mockRepo) CreateTripEvent(_ context.Context, input CreateTripEventInput) (*TripEvent, error) {
	m.nextEventID++
	event := &TripEvent{
		ID:              m.nextEventID,
		DispatchID:      input.DispatchID,
		EventType:       input.EventType,
		Latitude:        input.Latitude,
		Longitude:       input.Longitude,
		CreatedByUserID: input.CreatedByUserID,
		CreatedAt:       time.Now(),
	}
	m.tripEvents = append(m.tripEvents, *event)
	return event, nil
}

func (m *mockRepo) ListTripEvents(_ context.Context, _ int64) ([]TripEvent, error) {
	return m.tripEvents, nil
}

func (m *mockRepo) OrderAccess(_ context.Context, orderID int64) (*OrderAccess, error) {
	access, ok := m.orderAccess[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	return access, nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID int64, userID int64) (bool, error) {
	for _, id := range m.nurseryMembers[nurseryID] {
		if id == userID {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockRepo) IsDispatchDriver(_ context.Context, driverID int64, userID int64) (bool, error) {
	uid, ok := m.driverUserIDs[driverID]
	if !ok {
		return false, nil
	}
	return uid == userID, nil
}

func (m *mockRepo) GetOwnedNurseryID(_ context.Context, userID int64) (*int64, error) {
	id, ok := m.ownedNurseries[userID]
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func (m *mockRepo) GetUserNurseryIDs(_ context.Context, userID int64) ([]int64, error) {
	return m.userNurseries[userID], nil
}

// ── actor helpers ─────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func driverActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }

func nid(n int64) *int64 { return &n }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_AdminForbidden(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedOrderAccess(10, &nurseryID)
	_, err := svc(repo).Create(context.Background(), adminActor(1), CreateDispatchRequest{OrderID: 10})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("admin create dispatch: want ErrForbidden, got %v", err)
	}
}

func TestCreate_OwnerSuccess(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100)
	repo.seedOrderAccess(10, &nurseryID)
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateDispatchRequest{OrderID: 10})
	if err != nil {
		t.Errorf("owner create dispatch: %v", err)
	}
}

func TestCreate_ManagerForbidden(t *testing.T) {
	// canAccessOrder only permits NURSERY_OWNER — managers cannot create dispatches.
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100, 200)
	repo.seedOrderAccess(10, &nurseryID)
	_, err := svc(repo).Create(context.Background(), managerActor(200), CreateDispatchRequest{OrderID: 10})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("manager create dispatch: want ErrForbidden, got %v", err)
	}
}

func TestCreate_StrangerForbidden(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100)
	repo.seedOrderAccess(10, &nurseryID)
	_, err := svc(repo).Create(context.Background(), ownerActor(999), CreateDispatchRequest{OrderID: 10})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger create dispatch: want ErrForbidden, got %v", err)
	}
}

func TestCreate_DuplicateDispatchNumber(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100)
	repo.seedOrderAccess(10, &nurseryID)
	num := "DN-001"
	repo.duplicates[num] = true
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateDispatchRequest{
		OrderID:        10,
		DispatchNumber: &num,
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate dispatch number: want ErrDuplicate, got %v", err)
	}
}

func TestCreate_MissingOrderID(t *testing.T) {
	repo := newMock()
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateDispatchRequest{OrderID: 0})
	// OrderID 0 → OrderAccess lookup fails → ErrNotFound (or ErrInvalidInput)
	if err == nil {
		t.Error("missing order_id: want error, got nil")
	}
}

// ── UpdateStatus ──────────────────────────────────────────────────────────────

func TestUpdateStatus_PendingToDispatched(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	d, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "DISPATCHED"})
	if err != nil {
		t.Fatalf("PENDING→DISPATCHED: %v", err)
	}
	if d.Status != "DISPATCHED" {
		t.Errorf("status: want DISPATCHED, got %s", d.Status)
	}
}

func TestUpdateStatus_InTransitToDelivered(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "DELIVERED"})
	if err != nil {
		t.Errorf("IN_TRANSIT→DELIVERED: %v", err)
	}
}

func TestUpdateStatus_InvalidTransitionDeliveredToInTransit(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "DELIVERED", SellerNurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "IN_TRANSIT"})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("DELIVERED→IN_TRANSIT: want ErrInvalidStatus, got %v", err)
	}
}

func TestUpdateStatus_UnknownStatusRejected(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "FLYING"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown status: want ErrInvalidInput, got %v", err)
	}
}

func TestUpdateStatus_StrangerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(999), 10, UpdateStatusRequest{Status: "DISPATCHED"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger update status: want ErrForbidden, got %v", err)
	}
}

// ── AcceptDispatch ────────────────────────────────────────────────────────────

func TestAcceptDispatch_OnlyDriverAllowed(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), ownerActor(100), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("owner accept dispatch: want ErrForbidden, got %v", err)
	}
}

func TestAcceptDispatch_ManagerForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), managerActor(200), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("manager accept dispatch: want ErrForbidden, got %v", err)
	}
}

func TestAcceptDispatch_BuyerForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), buyerActor(300), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("buyer accept dispatch: want ErrForbidden, got %v", err)
	}
}

func TestAcceptDispatch_DriverSuccess(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	d, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10)
	if err != nil {
		t.Fatalf("driver accept dispatch: %v", err)
	}
	if d.Status != "ACCEPTED" {
		t.Errorf("status after accept: want ACCEPTED, got %s", d.Status)
	}
}

func TestAcceptDispatch_AlreadyAcceptedByDifferentDriver(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	existing := int64(401)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid, DriverUserID: &existing})
	_, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10)
	if !errors.Is(err, ErrAlreadyAccepted) {
		t.Errorf("already accepted by other driver: want ErrAlreadyAccepted, got %v", err)
	}
}

func TestAcceptDispatch_NonPendingForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("accept non-pending: want ErrForbidden, got %v", err)
	}
}

func TestAcceptDispatch_IdempotentForSameDriver(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	uid := int64(400)
	repo.seedDispatch(Dispatch{ID: 10, Status: "ACCEPTED", SellerNurseryID: &nid, DriverUserID: &uid})
	d, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10)
	if err != nil {
		t.Errorf("idempotent accept: %v", err)
	}
	if d.Status != "ACCEPTED" {
		t.Errorf("status: want ACCEPTED, got %s", d.Status)
	}
}

// ── CreateTripEvent ───────────────────────────────────────────────────────────

func TestCreateTripEvent_AssignedDriverSuccess(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	uid := int64(400)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid, DriverUserID: &uid})
	_, err := svc(repo).CreateTripEvent(context.Background(), driverActor(400), 10, CreateTripEventRequest{
		EventType: "CHECKPOINT",
	})
	if err != nil {
		t.Errorf("assigned driver trip event: %v", err)
	}
}

func TestCreateTripEvent_NonDriverForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedNursery(1, 100)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid})
	_, err := svc(repo).CreateTripEvent(context.Background(), ownerActor(100), 10, CreateTripEventRequest{
		EventType: "CHECKPOINT",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("owner trip event: want ErrForbidden, got %v", err)
	}
}

func TestCreateTripEvent_AdminSuccess(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid})
	_, err := svc(repo).CreateTripEvent(context.Background(), adminActor(1), 10, CreateTripEventRequest{
		EventType: "CHECKPOINT",
	})
	if err != nil {
		t.Errorf("admin trip event: %v", err)
	}
}

func TestCreateTripEvent_UnassignedDriverForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid}) // no driver assigned
	_, err := svc(repo).CreateTripEvent(context.Background(), driverActor(999), 10, CreateTripEventRequest{
		EventType: "CHECKPOINT",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("unassigned driver trip event: want ErrForbidden, got %v", err)
	}
}

// ── CreateItem ────────────────────────────────────────────────────────────────

func TestCreateItem_ValidQuantity(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedNursery(1, 100)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, DispatchItemRequest{Quantity: 5})
	if err != nil {
		t.Errorf("create item: %v", err)
	}
}

func TestCreateItem_ZeroQuantityRejected(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedNursery(1, 100)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, DispatchItemRequest{Quantity: 0})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("zero qty item: want ErrInvalidInput, got %v", err)
	}
}

func TestCreateItem_StrangerForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedNursery(1, 100)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(999), 10, DispatchItemRequest{Quantity: 5})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger create item: want ErrForbidden, got %v", err)
	}
}

// ── GetByCode (public track) ──────────────────────────────────────────────────

func TestGetByCode_Found(t *testing.T) {
	repo := newMock()
	repo.seedDispatch(Dispatch{ID: 10, DispatchCode: "DC-XYZ", Status: "IN_TRANSIT"})
	d, err := svc(repo).GetByCode(context.Background(), "DC-XYZ")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if d.ID != 10 {
		t.Errorf("id: want 10, got %d", d.ID)
	}
}

func TestGetByCode_NotFound(t *testing.T) {
	repo := newMock()
	_, err := svc(repo).GetByCode(context.Background(), "BOGUS")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByCode not found: want ErrNotFound, got %v", err)
	}
}

// ── State machine transition table ───────────────────────────────────────────

func TestValidDispatchTransitions(t *testing.T) {
	cases := []struct {
		from  string
		to    string
		valid bool
	}{
		{"PENDING", "DISPATCHED", true},
		{"PENDING", "IN_TRANSIT", true},
		{"PENDING", "CANCELLED", true},
		{"PENDING", "DELIVERED", false},
		{"ACCEPTED", "DISPATCHED", true},
		{"ACCEPTED", "IN_TRANSIT", true},
		{"ACCEPTED", "CANCELLED", true},
		{"DISPATCHED", "IN_TRANSIT", true},
		{"DISPATCHED", "CANCELLED", true},
		{"DISPATCHED", "DELIVERED", false},
		{"IN_TRANSIT", "DELIVERED", true},
		{"IN_TRANSIT", "CANCELLED", true},
		{"IN_TRANSIT", "PENDING", false},
		{"DELIVERED", "IN_TRANSIT", false},
		{"DELIVERED", "CANCELLED", false},
		{"CANCELLED", "PENDING", false},
	}
	for _, c := range cases {
		got := validTransition(c.from, c.to)
		if got != c.valid {
			t.Errorf("transition %s→%s: want valid=%v, got %v", c.from, c.to, c.valid, got)
		}
	}
}

func TestIsAllowedStatus(t *testing.T) {
	allowed := []string{"PENDING", "ACCEPTED", "DISPATCHED", "IN_TRANSIT", "DELIVERED", "CANCELLED"}
	notAllowed := []string{"LOADING", "COMPLETED", "UNICORN", ""}
	for _, s := range allowed {
		if !isAllowedStatus(s) {
			t.Errorf("isAllowedStatus(%s): want true", s)
		}
	}
	for _, s := range notAllowed {
		if isAllowedStatus(s) {
			t.Errorf("isAllowedStatus(%s): want false", s)
		}
	}
}

// ── Not found ─────────────────────────────────────────────────────────────────

func TestGet_NotFound(t *testing.T) {
	repo := newMock()
	_, err := svc(repo).Get(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get not found: want ErrNotFound, got %v", err)
	}
}

func TestUpdateStatus_NotFound(t *testing.T) {
	repo := newMock()
	_, err := svc(repo).UpdateStatus(context.Background(), adminActor(1), 9999, UpdateStatusRequest{Status: "DISPATCHED"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateStatus not found: want ErrNotFound, got %v", err)
	}
}
