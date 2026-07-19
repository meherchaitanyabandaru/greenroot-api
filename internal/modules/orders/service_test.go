package orders

import (
	"context"
	"errors"
	"testing"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	orders         map[int64]*Order
	items          map[int64]*OrderItem
	deliveries     map[int64]*DeliverySnapshot
	nurseryOwners  map[int64]int64   // nursery_id → owner_user_id
	nurseryMembers map[int64][]int64 // nursery_id → []user_id
	userNurseries  map[int64][]int64 // user_id → []nursery_id (all memberships)
	ownedNurseries map[int64]int64   // user_id → nursery_id (owners only)
	driverUserIDs  map[int64]int64   // order_id → driver user_id
	activeDispatch map[int64]ActiveDispatchSummary
	startedOrders  map[int64]bool
	undelivered    map[int64]bool
	notifications  []int64
	nextID         int64
	nextItemID     int64
}

func newMock() *mockRepo {
	return &mockRepo{
		orders:         make(map[int64]*Order),
		items:          make(map[int64]*OrderItem),
		deliveries:     make(map[int64]*DeliverySnapshot),
		nurseryOwners:  make(map[int64]int64),
		nurseryMembers: make(map[int64][]int64),
		userNurseries:  make(map[int64][]int64),
		ownedNurseries: make(map[int64]int64),
		driverUserIDs:  make(map[int64]int64),
		activeDispatch: make(map[int64]ActiveDispatchSummary),
		startedOrders:  make(map[int64]bool),
		undelivered:    make(map[int64]bool),
	}
}

// seedNursery sets up owner and optional extra members for a nursery.
func (m *mockRepo) seedNursery(nurseryID, ownerID int64, members ...int64) {
	m.nurseryOwners[nurseryID] = ownerID
	m.ownedNurseries[ownerID] = nurseryID
	all := append([]int64{ownerID}, members...)
	m.nurseryMembers[nurseryID] = all
	for _, uid := range all {
		m.userNurseries[uid] = append(m.userNurseries[uid], nurseryID)
	}
}

func (m *mockRepo) seedOrder(o Order) {
	m.orders[o.ID] = &o
}

func (m *mockRepo) seedItem(item OrderItem) {
	m.items[item.ID] = &item
}

func (m *mockRepo) nextOrderID() int64 {
	m.nextID++
	return m.nextID
}

// Repository interface implementation

func (m *mockRepo) List(_ context.Context, _ ListOrdersRequest) ([]Order, int64, error) {
	result := make([]Order, 0, len(m.orders))
	for _, o := range m.orders {
		result = append(result, *o)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*Order, error) {
	o, ok := m.orders[id]
	if !ok {
		return nil, ErrNotFound
	}
	// return a copy with items attached
	clone := *o
	for _, item := range m.items {
		if item.OrderID == id {
			clone.Items = append(clone.Items, *item)
		}
	}
	return &clone, nil
}

func (m *mockRepo) Create(_ context.Context, actorID int64, input CreateOrderRequest, orderNumber string) (*Order, error) {
	id := m.nextOrderID()
	nurseryID := input.SellerNurseryID
	o := &Order{
		ID:              id,
		OrderCode:       "ORD-TEST-001",
		OrderNumber:     orderNumber,
		Status:          input.Status,
		SellerNurseryID: nurseryID,
		NurseryID:       nurseryID,
		BuyerUserID:     input.BuyerUserID,
		CreatedByUserID: &actorID,
	}
	m.orders[id] = o
	return o, nil
}

func (m *mockRepo) GetDeliverySnapshot(_ context.Context, orderID int64) (*DeliverySnapshot, error) {
	snapshot, ok := m.deliveries[orderID]
	if !ok {
		return nil, nil
	}
	clone := *snapshot
	return &clone, nil
}

func (m *mockRepo) UpdateDeliverySnapshot(_ context.Context, orderID int64, actorID int64, input DeliverySnapshotRequest) (*DeliverySnapshot, error) {
	if _, ok := m.orders[orderID]; !ok {
		return nil, ErrNotFound
	}
	snapshot := &DeliverySnapshot{
		ID:                   orderID,
		OrderID:              orderID,
		ContactName:          input.ContactName,
		ContactMobile:        input.ContactMobile,
		AlternateMobile:      input.AlternateMobile,
		AddressLine1:         input.AddressLine1,
		AddressLine2:         input.AddressLine2,
		City:                 input.City,
		State:                input.State,
		Country:              input.Country,
		PostalCode:           input.PostalCode,
		Landmark:             input.Landmark,
		DeliveryInstructions: input.DeliveryInstructions,
		Latitude:             input.Latitude,
		Longitude:            input.Longitude,
		GPSAccuracyM:         input.GPSAccuracyM,
		LocationSource:       input.LocationSource,
		ConfirmedBy:          &actorID,
		EmergencyUpdated:     input.EmergencyUpdate,
		RequiresDriverAck:    input.EmergencyUpdate,
	}
	m.deliveries[orderID] = snapshot
	m.orders[orderID].DeliverySnapshot = snapshot
	return snapshot, nil
}

func (m *mockRepo) OrderHasStartedDispatch(_ context.Context, orderID int64) (bool, error) {
	return m.startedOrders[orderID], nil
}

func (m *mockRepo) OrderHasUndeliveredDispatch(_ context.Context, orderID int64) (bool, error) {
	return m.undelivered[orderID], nil
}

func (m *mockRepo) ActiveDispatchForOrder(_ context.Context, orderID int64) (*ActiveDispatchSummary, error) {
	summary, ok := m.activeDispatch[orderID]
	if !ok {
		return nil, nil
	}
	return &summary, nil
}

func (m *mockRepo) StartedDispatchDriverUserID(_ context.Context, orderID int64) (*int64, error) {
	userID, ok := m.driverUserIDs[orderID]
	if !ok {
		return nil, nil
	}
	return &userID, nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, _ int64, orderID int64, status string) (*Order, error) {
	o, ok := m.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	o.Status = status
	return o, nil
}

func (m *mockRepo) UpdateStatusWithLoading(_ context.Context, _ int64, orderID int64, status string, _ string) (*Order, error) {
	o, ok := m.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	o.Status = status
	return o, nil
}

func (m *mockRepo) Cancel(_ context.Context, _ int64, orderID int64, reason string) (*Order, error) {
	o, ok := m.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	o.Status = "CANCELLED"
	o.CancelReason = &reason
	return o, nil
}

func (m *mockRepo) AssignManager(_ context.Context, orderID int64, managerUserID int64) (*Order, error) {
	o, ok := m.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	o.AssignedManagerUserID = &managerUserID
	return o, nil
}

func (m *mockRepo) Delete(_ context.Context, orderID int64) error {
	if _, ok := m.orders[orderID]; !ok {
		return ErrNotFound
	}
	delete(m.orders, orderID)
	return nil
}

func (m *mockRepo) ListItems(_ context.Context, orderID int64) ([]OrderItem, error) {
	var result []OrderItem
	for _, item := range m.items {
		if item.OrderID == orderID {
			result = append(result, *item)
		}
	}
	return result, nil
}

func (m *mockRepo) FindItem(_ context.Context, itemID int64) (*OrderItem, error) {
	item, ok := m.items[itemID]
	if !ok {
		return nil, ErrNotFound
	}
	return item, nil
}

func (m *mockRepo) CreateItem(_ context.Context, orderID int64, input OrderItemRequest) (*OrderItem, error) {
	m.nextItemID++
	item := &OrderItem{
		ID:         m.nextItemID,
		OrderID:    orderID,
		PlantID:    input.PlantID,
		Quantity:   input.Quantity,
		UnitPrice:  input.UnitPrice,
		TotalPrice: input.TotalPrice,
	}
	m.items[item.ID] = item
	return item, nil
}

func (m *mockRepo) UpdateItem(_ context.Context, itemID int64, input OrderItemRequest) (*OrderItem, error) {
	item, ok := m.items[itemID]
	if !ok {
		return nil, ErrNotFound
	}
	item.Quantity = input.Quantity
	item.UnitPrice = input.UnitPrice
	item.TotalPrice = input.TotalPrice
	return item, nil
}

func (m *mockRepo) DeleteItem(_ context.Context, itemID int64) error {
	if _, ok := m.items[itemID]; !ok {
		return ErrNotFound
	}
	delete(m.items, itemID)
	return nil
}

func (m *mockRepo) SetLoadedQuantity(_ context.Context, itemID int64, qty float64) (*OrderItem, error) {
	item, ok := m.items[itemID]
	if !ok {
		return nil, ErrNotFound
	}
	item.LoadedQuantity = &qty
	return item, nil
}

func (m *mockRepo) RecalculateTotalFromLoaded(_ context.Context, _ int64) error { return nil }

func (m *mockRepo) CreateNotification(_ context.Context, userID int64, _, _, _ string) error {
	m.notifications = append(m.notifications, userID)
	return nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID int64, userID int64) (bool, error) {
	members := m.nurseryMembers[nurseryID]
	for _, id := range members {
		if id == userID {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockRepo) IsNurseryOwner(_ context.Context, nurseryID int64, userID int64) (bool, error) {
	owner, ok := m.nurseryOwners[nurseryID]
	if !ok {
		return false, nil
	}
	return owner == userID, nil
}

func (m *mockRepo) GetUserNurseryIDs(_ context.Context, userID int64) ([]int64, error) {
	return m.userNurseries[userID], nil
}

func (m *mockRepo) GetOwnedNurseryID(_ context.Context, userID int64) (*int64, error) {
	id, ok := m.ownedNurseries[userID]
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func (m *mockRepo) FindOrCreateBuyerByMobile(_ context.Context, _ string, _ string) (int64, error) {
	m.nextID++
	return m.nextID, nil
}

// ── actor helpers ─────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func nurseryID(n int64) *int64 { return &n }
func userID(n int64) *int64    { return &n }
func str(s string) *string     { return &s }

func validItem() OrderItemRequest {
	return OrderItemRequest{PlantID: 1, Quantity: 10, UnitPrice: 50, TotalPrice: 500}
}

func testDeliverySnapshot() *DeliverySnapshot {
	return &DeliverySnapshot{AddressLine1: str("Delivery address")}
}

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_AdminForbidden(t *testing.T) {
	repo := newMock()
	_, err := svc(repo).Create(context.Background(), adminActor(1), CreateOrderRequest{
		BuyerUserID:     userID(10),
		SellerNurseryID: nurseryID(1),
		Status:          "PENDING",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("admin create: want ErrForbidden, got %v", err)
	}
}

func TestCreate_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateOrderRequest{
		BuyerUserID:     userID(200),
		SellerNurseryID: nurseryID(1),
		Status:          "PENDING",
		Items:           []OrderItemRequest{validItem()},
	})
	if err != nil {
		t.Errorf("owner create: unexpected error: %v", err)
	}
}

func TestCreate_ManagerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100, 200)
	_, err := svc(repo).Create(context.Background(), managerActor(200), CreateOrderRequest{
		BuyerUserID:     userID(300),
		SellerNurseryID: nurseryID(1),
		Status:          "PENDING",
		Items:           []OrderItemRequest{validItem()},
	})
	if err != nil {
		t.Errorf("manager create: unexpected error: %v", err)
	}
}

func TestCreate_OwnerNotMemberForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100) // ownerID=100, stranger ownerID=999
	_, err := svc(repo).Create(context.Background(), ownerActor(999), CreateOrderRequest{
		BuyerUserID:     userID(200),
		SellerNurseryID: nurseryID(1),
		Status:          "PENDING",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger owner: want ErrForbidden, got %v", err)
	}
}

func TestCreate_BuyerCreatesOwnOrder(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	buyerID := int64(300)
	_, err := svc(repo).Create(context.Background(), buyerActor(buyerID), CreateOrderRequest{
		SellerNurseryID: nurseryID(1),
		// BuyerUserID omitted — normalizeCreate fills it from actor
	})
	if err != nil {
		t.Errorf("buyer self-create: unexpected error: %v", err)
	}
}

func TestCreate_BuyerCreatesForAnotherBuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	actor := buyerActor(300)
	other := int64(400)
	_, err := svc(repo).Create(context.Background(), actor, CreateOrderRequest{
		BuyerUserID:     &other, // different from actor
		SellerNurseryID: nurseryID(1),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("buyer for other: want ErrForbidden, got %v", err)
	}
}

func TestCreate_MissingSellerNursery(t *testing.T) {
	repo := newMock()
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateOrderRequest{
		BuyerUserID: userID(200),
		Status:      "PENDING",
		// SellerNurseryID missing
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("missing nursery: want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_InvalidItemNegativeQty(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateOrderRequest{
		BuyerUserID:     userID(200),
		SellerNurseryID: nurseryID(1),
		Status:          "PENDING",
		Items:           []OrderItemRequest{{PlantID: 1, Quantity: -1, UnitPrice: 50, TotalPrice: -50}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("negative qty: want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_InvalidItemPriceMismatch(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateOrderRequest{
		BuyerUserID:     userID(200),
		SellerNurseryID: nurseryID(1),
		Status:          "PENDING",
		Items:           []OrderItemRequest{{PlantID: 1, Quantity: 10, UnitPrice: 50, TotalPrice: 999}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("price mismatch: want ErrInvalidInput, got %v", err)
	}
}

// ── Delivery snapshot ────────────────────────────────────────────────────────

func TestUpdateDeliverySnapshot_EmergencyNotifiesStartedDispatchDriver(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	repo.seedOrder(Order{
		ID:              10,
		OrderCode:       "ORD-TEST-001",
		Status:          "LOADED",
		SellerNurseryID: nurseryID(1),
		NurseryID:       nurseryID(1),
	})
	repo.startedOrders[10] = true
	repo.driverUserIDs[10] = 700

	_, err := svc(repo).UpdateDeliverySnapshot(context.Background(), ownerActor(100), 10, DeliverySnapshotRequest{
		ContactName:     str("Customer"),
		ContactMobile:   str("9000000000"),
		AddressLine1:    str("Updated delivery address"),
		City:            str("Hyderabad"),
		State:           str("Telangana"),
		Country:         str("India"),
		PostalCode:      str("500032"),
		EmergencyUpdate: true,
	})
	if err != nil {
		t.Fatalf("emergency delivery update: %v", err)
	}
	if len(repo.notifications) != 1 || repo.notifications[0] != 700 {
		t.Fatalf("driver notification: got %#v, want [700]", repo.notifications)
	}
	if got := repo.deliveries[10].LocationSource; got == nil || *got != "admin_updated" {
		t.Fatalf("location source: got %v, want admin_updated", got)
	}
}

// ── UpdateStatus (generic PUT) ────────────────────────────────────────────────

func TestUpdateStatus_PendingToConfirmed(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid, DeliverySnapshot: testDeliverySnapshot()})
	o, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "CONFIRMED"})
	if err != nil {
		t.Fatalf("PENDING→CONFIRMED: %v", err)
	}
	if o.Status != "CONFIRMED" {
		t.Errorf("status: want CONFIRMED, got %s", o.Status)
	}
}

func TestUpdateStatus_PendingToConfirmedRequiresDeliveryAddress(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "CONFIRMED"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("PENDING→CONFIRMED without delivery: want ErrInvalidInput, got %v", err)
	}
}

func TestUpdateStatus_LoadingNotAllowedViaGeneric(t *testing.T) {
	// LOADING is not in isAllowedStatus — generic PUT rejects it as ErrInvalidInput.
	// StartLoading is the dedicated endpoint that transitions to LOADING.
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "LOADING"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("LOADING via generic PUT: want ErrInvalidInput, got %v", err)
	}
}

func TestUpdateStatus_ConfirmedToCompletedBlocked(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "COMPLETED"})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("CONFIRMED→COMPLETED: want ErrInvalidStatus, got %v", err)
	}
}

func TestUpdateStatus_LoadedToCompleted(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADED", NurseryID: &nid})
	o, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "COMPLETED"})
	if err != nil {
		t.Fatalf("LOADED→COMPLETED: %v", err)
	}
	if o.Status != "COMPLETED" {
		t.Errorf("status: want COMPLETED, got %s", o.Status)
	}
}

func TestUpdateStatus_LoadedToCompletedBlockedByUndeliveredDispatch(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADED", NurseryID: &nid})
	repo.undelivered[10] = true

	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "COMPLETED"})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("LOADED→COMPLETED with undelivered dispatch: want ErrInvalidStatus, got %v", err)
	}
}

func TestUpdateStatus_PartiallyFulfilledToCompleted(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PARTIALLY_FULFILLED", NurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "COMPLETED"})
	if err != nil {
		t.Errorf("PARTIALLY_FULFILLED→COMPLETED: %v", err)
	}
}

func TestUpdateStatus_CompletedIsTerminal(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "COMPLETED", NurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "CONFIRMED"})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("COMPLETED→CONFIRMED: want ErrInvalidStatus, got %v", err)
	}
}

func TestUpdateStatus_UnknownStatus(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "UNICORN"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad status: want ErrInvalidInput, got %v", err)
	}
}

func TestUpdateStatus_StrangerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(999), 10, UpdateStatusRequest{Status: "CONFIRMED"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger update: want ErrForbidden, got %v", err)
	}
}

func TestUpdateStatus_AssignedManagerCanUpdate(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	mgr := int64(200)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid, AssignedManagerUserID: &mgr, DeliverySnapshot: testDeliverySnapshot()})
	_, err := svc(repo).UpdateStatus(context.Background(), managerActor(200), 10, UpdateStatusRequest{Status: "CONFIRMED"})
	if err != nil {
		t.Errorf("assigned manager update: %v", err)
	}
}

// ── StartLoading ──────────────────────────────────────────────────────────────

func TestStartLoading_FromConfirmed(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid, DeliverySnapshot: testDeliverySnapshot()})
	o, err := svc(repo).StartLoading(context.Background(), ownerActor(100), 10)
	if err != nil {
		t.Fatalf("StartLoading from CONFIRMED: %v", err)
	}
	if o.Status != "LOADING" {
		t.Errorf("status: want LOADING, got %s", o.Status)
	}
}

func TestStartLoading_FromPending(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).StartLoading(context.Background(), ownerActor(100), 10)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("StartLoading from PENDING: want ErrInvalidStatus, got %v", err)
	}
}

func TestStartLoading_NonMemberForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid})
	_, err := svc(repo).StartLoading(context.Background(), ownerActor(999), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member StartLoading: want ErrForbidden, got %v", err)
	}
}

// ── CompleteLoading ───────────────────────────────────────────────────────────

func TestCompleteLoading_AllFullBecomesLoaded(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADING", NurseryID: &nid})
	full := float64(10)
	repo.seedItem(OrderItem{ID: 1, OrderID: 10, Quantity: 10, LoadedQuantity: &full})
	o, err := svc(repo).CompleteLoading(context.Background(), ownerActor(100), 10)
	if err != nil {
		t.Fatalf("CompleteLoading all full: %v", err)
	}
	if o.Status != "LOADED" {
		t.Errorf("status: want LOADED, got %s", o.Status)
	}
}

func TestCompleteLoading_ShortBecomesPartiallyFulfilled(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADING", NurseryID: &nid})
	partial := float64(5) // ordered 10, loaded only 5
	repo.seedItem(OrderItem{ID: 1, OrderID: 10, Quantity: 10, LoadedQuantity: &partial})
	o, err := svc(repo).CompleteLoading(context.Background(), ownerActor(100), 10)
	if err != nil {
		t.Fatalf("CompleteLoading partial: %v", err)
	}
	if o.Status != "PARTIALLY_FULFILLED" {
		t.Errorf("status: want PARTIALLY_FULFILLED, got %s", o.Status)
	}
}

func TestCompleteLoading_NotInLoading(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid})
	_, err := svc(repo).CompleteLoading(context.Background(), ownerActor(100), 10)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("CompleteLoading not in LOADING: want ErrInvalidStatus, got %v", err)
	}
}

// ── SetLoadedQuantity ─────────────────────────────────────────────────────────

func TestSetLoadedQuantity_ValidInLoading(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADING", NurseryID: &nid})
	repo.seedItem(OrderItem{ID: 1, OrderID: 10, Quantity: 10})
	item, err := svc(repo).SetLoadedQuantity(context.Background(), ownerActor(100), 10, 1, 7)
	if err != nil {
		t.Fatalf("SetLoadedQuantity: %v", err)
	}
	if item.LoadedQuantity == nil || *item.LoadedQuantity != 7 {
		t.Errorf("loaded qty: want 7, got %v", item.LoadedQuantity)
	}
}

func TestSetLoadedQuantity_NegativeRejected(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADING", NurseryID: &nid})
	repo.seedItem(OrderItem{ID: 1, OrderID: 10, Quantity: 10})
	_, err := svc(repo).SetLoadedQuantity(context.Background(), ownerActor(100), 10, 1, -1)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("negative loaded qty: want ErrInvalidInput, got %v", err)
	}
}

func TestSetLoadedQuantity_NotInLoading(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	repo.seedItem(OrderItem{ID: 1, OrderID: 10, Quantity: 10})
	_, err := svc(repo).SetLoadedQuantity(context.Background(), ownerActor(100), 10, 1, 5)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("not in LOADING: want ErrInvalidStatus, got %v", err)
	}
}

func TestSetLoadedQuantity_WrongOrderItem(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADING", NurseryID: &nid})
	repo.seedItem(OrderItem{ID: 1, OrderID: 99, Quantity: 10}) // item belongs to order 99, not 10
	_, err := svc(repo).SetLoadedQuantity(context.Background(), ownerActor(100), 10, 1, 5)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("wrong order item: want ErrForbidden, got %v", err)
	}
}

// ── Cancel ────────────────────────────────────────────────────────────────────

func TestCancel_PendingByOwner(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	o, err := svc(repo).Cancel(context.Background(), ownerActor(100), 10, "test")
	if err != nil {
		t.Fatalf("cancel PENDING by owner: %v", err)
	}
	if o.Status != "CANCELLED" {
		t.Errorf("status: want CANCELLED, got %s", o.Status)
	}
}

func TestCancel_PendingByBuyerSelf(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	bid := int64(300)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid, BuyerUserID: &bid})
	_, err := svc(repo).Cancel(context.Background(), buyerActor(300), 10, "changed mind")
	if err != nil {
		t.Errorf("buyer self-cancel PENDING: %v", err)
	}
}

func TestCancel_ConfirmedByBuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	bid := int64(300)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid, BuyerUserID: &bid})
	_, err := svc(repo).Cancel(context.Background(), buyerActor(300), 10, "oops")
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("buyer cancel CONFIRMED: want ErrForbidden, got %v", err)
	}
}

func TestCancel_LoadedForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADED", NurseryID: &nid})
	_, err := svc(repo).Cancel(context.Background(), ownerActor(100), 10, "cancel")
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("cancel LOADED: want ErrInvalidStatus, got %v", err)
	}
}

func TestCancel_PartiallyFulfilledForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PARTIALLY_FULFILLED", NurseryID: &nid})
	_, err := svc(repo).Cancel(context.Background(), ownerActor(100), 10, "cancel")
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("cancel PARTIALLY_FULFILLED: want ErrInvalidStatus, got %v", err)
	}
}

func TestCancel_CompletedForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "COMPLETED", NurseryID: &nid})
	_, err := svc(repo).Cancel(context.Background(), ownerActor(100), 10, "cancel")
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("cancel COMPLETED: want ErrInvalidStatus, got %v", err)
	}
}

func TestCancel_AlreadyCancelledForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CANCELLED", NurseryID: &nid})
	_, err := svc(repo).Cancel(context.Background(), ownerActor(100), 10, "cancel")
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("cancel CANCELLED: want ErrInvalidStatus, got %v", err)
	}
}

func TestCancel_ConfirmedByManager(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100, 200)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid})
	_, err := svc(repo).Cancel(context.Background(), managerActor(200), 10, "cancel")
	if err != nil {
		t.Errorf("manager cancel CONFIRMED: %v", err)
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_PendingByOwner(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	if err := svc(repo).Delete(context.Background(), ownerActor(100), 10); err != nil {
		t.Errorf("delete PENDING: %v", err)
	}
	if _, ok := repo.orders[10]; ok {
		t.Error("order still in store after delete")
	}
}

func TestDelete_ConfirmedBlocked(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid})
	err := svc(repo).Delete(context.Background(), ownerActor(100), 10)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("delete CONFIRMED: want ErrInvalidStatus, got %v", err)
	}
}

func TestDelete_StrangerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	err := svc(repo).Delete(context.Background(), ownerActor(999), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger delete: want ErrForbidden, got %v", err)
	}
}

// ── AssignManager ─────────────────────────────────────────────────────────────

func TestAssignManager_OwnerCanAssign(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	o, err := svc(repo).AssignManager(context.Background(), ownerActor(100), 10, 200)
	if err != nil {
		t.Fatalf("owner assign manager: %v", err)
	}
	if o.AssignedManagerUserID == nil || *o.AssignedManagerUserID != 200 {
		t.Error("manager not assigned")
	}
}

func TestAssignManager_AdminCanAssign(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).AssignManager(context.Background(), adminActor(1), 10, 200)
	if err != nil {
		t.Errorf("admin assign manager: %v", err)
	}
}

func TestAssignManager_ManagerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100, 200)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).AssignManager(context.Background(), managerActor(200), 10, 300)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("manager assign manager: want ErrForbidden, got %v", err)
	}
}

// ── Item CRUD + lock enforcement ──────────────────────────────────────────────

func TestCreateItem_AllowedInPending(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, validItem())
	if err != nil {
		t.Errorf("create item in PENDING: %v", err)
	}
}

func TestCreateItem_AllowedInConfirmed(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "CONFIRMED", NurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, validItem())
	if err != nil {
		t.Errorf("create item in CONFIRMED: %v", err)
	}
}

func TestCreateItem_AllowedInLoading(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADING", NurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, validItem())
	if err != nil {
		t.Errorf("create item in LOADING: %v", err)
	}
}

func TestCreateItem_LockedAfterLoaded(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADED", NurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, validItem())
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("create item in LOADED: want ErrInvalidStatus, got %v", err)
	}
}

func TestCreateItem_LockedAfterCompleted(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "COMPLETED", NurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, validItem())
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("create item in COMPLETED: want ErrInvalidStatus, got %v", err)
	}
}

func TestCreateItem_InvalidZeroQty(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	_, err := svc(repo).CreateItem(context.Background(), ownerActor(100), 10, OrderItemRequest{
		PlantID: 1, Quantity: 0, UnitPrice: 50, TotalPrice: 0,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("zero qty item: want ErrInvalidInput, got %v", err)
	}
}

func TestDeleteItem_AllowedInPending(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "PENDING", NurseryID: &nid})
	repo.seedItem(OrderItem{ID: 1, OrderID: 10, Quantity: 5})
	if err := svc(repo).DeleteItem(context.Background(), ownerActor(100), 1); err != nil {
		t.Errorf("delete item in PENDING: %v", err)
	}
}

func TestDeleteItem_LockedAfterLoaded(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedOrder(Order{ID: 10, Status: "LOADED", NurseryID: &nid})
	repo.seedItem(OrderItem{ID: 1, OrderID: 10, Quantity: 5})
	err := svc(repo).DeleteItem(context.Background(), ownerActor(100), 1)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("delete item in LOADED: want ErrInvalidStatus, got %v", err)
	}
}

// ── ScopeList (RBAC scoping) ──────────────────────────────────────────────────

func TestScopeList_AdminSeesAll(t *testing.T) {
	input := ListOrdersRequest{Page: 1, PerPage: 20}
	repo := newMock()
	if err := svc(repo).scopeList(context.Background(), adminActor(1), &input); err != nil {
		t.Errorf("admin scopeList: %v", err)
	}
	// Admin: no nursery filter applied
	if input.NurseryID != 0 && input.BuyerID != 0 {
		t.Error("admin should not have scope filters")
	}
}

func TestScopeList_OwnerScopedToNursery(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	input := ListOrdersRequest{Page: 1, PerPage: 20}
	if err := svc(repo).scopeList(context.Background(), ownerActor(100), &input); err != nil {
		t.Errorf("owner scopeList: %v", err)
	}
	if input.NurseryID != 1 {
		t.Errorf("owner scope: want nursery_id=1, got %d", input.NurseryID)
	}
}

func TestScopeList_BuyerScopedToSelf(t *testing.T) {
	repo := newMock()
	input := ListOrdersRequest{Page: 1, PerPage: 20}
	if err := svc(repo).scopeList(context.Background(), buyerActor(300), &input); err != nil {
		t.Errorf("buyer scopeList: %v", err)
	}
	if input.BuyerID != 300 {
		t.Errorf("buyer scope: want buyer_id=300, got %d", input.BuyerID)
	}
}

// ── State machine transition table ───────────────────────────────────────────

func TestValidOrderTransitions(t *testing.T) {
	cases := []struct {
		from  string
		to    string
		valid bool
	}{
		{"PENDING", "CONFIRMED", true},
		{"PENDING", "LOADING", false},
		{"PENDING", "LOADED", false},
		{"PENDING", "COMPLETED", false},
		{"CONFIRMED", "COMPLETED", false},
		{"CONFIRMED", "LOADED", false},
		{"LOADING", "LOADED", false}, // only via CompleteLoading
		{"LOADING", "COMPLETED", false},
		{"LOADED", "COMPLETED", true},
		{"PARTIALLY_FULFILLED", "COMPLETED", true},
		{"PARTIALLY_FULFILLED", "LOADED", false},
		{"COMPLETED", "CANCELLED", false},
		{"COMPLETED", "CONFIRMED", false},
		{"CANCELLED", "CONFIRMED", false},
	}
	for _, c := range cases {
		got := validOrderTransition(c.from, c.to)
		if got != c.valid {
			t.Errorf("transition %s→%s: want valid=%v, got %v", c.from, c.to, c.valid, got)
		}
	}
}

func TestIsEditableStatus(t *testing.T) {
	editable := []string{"PENDING", "CONFIRMED", "LOADING"}
	locked := []string{"LOADED", "PARTIALLY_FULFILLED", "COMPLETED", "CANCELLED"}
	for _, s := range editable {
		if !isEditableStatus(s) {
			t.Errorf("isEditableStatus(%s): want true", s)
		}
	}
	for _, s := range locked {
		if isEditableStatus(s) {
			t.Errorf("isEditableStatus(%s): want false", s)
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

func TestGet_EnrichesActiveDispatchLifecycle(t *testing.T) {
	repo := newMock()
	repo.seedOrder(Order{ID: 10, Status: "LOADED", OrderNumber: "GR-ORD-10"})
	repo.activeDispatch[10] = ActiveDispatchSummary{ID: 20, Status: "IN_TRANSIT"}

	order, err := svc(repo).Get(context.Background(), adminActor(1), 10)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if order.ActiveDispatchID == nil || *order.ActiveDispatchID != 20 {
		t.Fatalf("active dispatch id = %v, want 20", order.ActiveDispatchID)
	}
	if order.ActiveDispatchStatus == nil || *order.ActiveDispatchStatus != "IN_TRANSIT" {
		t.Fatalf("active dispatch status = %v, want IN_TRANSIT", order.ActiveDispatchStatus)
	}
	if order.Lifecycle == nil || order.Lifecycle.Customer.Label != "On the Way" {
		t.Fatalf("customer lifecycle = %#v, want On the Way", order.Lifecycle)
	}
	if got := order.Lifecycle.NextActions.Customer; len(got) != 1 || got[0] != "Track Delivery" {
		t.Fatalf("customer next actions = %#v, want Track Delivery", got)
	}
}

func TestDelete_NotFound(t *testing.T) {
	repo := newMock()
	err := svc(repo).Delete(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete not found: want ErrNotFound, got %v", err)
	}
}
