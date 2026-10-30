package dispatches

import (
	"context"
	"errors"
	"fmt"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/consent"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisgeo"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/rowversion"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
	"github.com/redis/go-redis/v9"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	dispatches     map[int64]*Dispatch
	items          map[int64]*DispatchItem
	tripEvents     []TripEvent
	orderAccess    map[int64]*OrderAccess      // order_id → access
	nurseryMembers map[int64][]int64           // nursery_id → []user_id
	ownedNurseries map[int64]int64             // user_id → nursery_id
	userNurseries  map[int64][]int64           // user_id → []nursery_id
	driverUserIDs  map[int64]int64             // driver_id → user_id
	drivers        map[int64]DriverEligibility // driver user_id → their driver record
	vehicles       map[int64][]DriverVehicle   // driver user_id → their lorry links
	held           map[int64]bool              // order_id → held by an admin
	duplicates     map[string]bool
	nextID         int64
	nextItemID     int64
	nextEventID    int64
	notified       []string // "<user id>:<type>" of in-app notifications
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
		drivers:        make(map[int64]DriverEligibility),
		vehicles:       make(map[int64][]DriverVehicle),
		held:           make(map[int64]bool),
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

// testTrip is every seeded trip's QR code (trip_uuid) unless the test sets one.
const testTrip = "0f8fad5b-d9cb-469f-a165-70867728950e"

func (m *mockRepo) seedDispatch(d Dispatch) {
	if d.TripUUID == nil {
		u := testTrip
		d.TripUUID = &u
	}
	m.dispatches[d.ID] = &d
}

func (m *mockRepo) seedOrderAccess(orderID int64, nurseryID *int64) {
	m.seedOrderAccessWithStatus(orderID, nurseryID, "LOADED")
}

func (m *mockRepo) seedOrderAccessWithStatus(orderID int64, nurseryID *int64, status string) {
	m.orderAccess[orderID] = &OrderAccess{OrderID: orderID, NurseryID: nurseryID, OrderStatus: status}
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

func (m *mockRepo) FindByTripUUID(_ context.Context, tripUUID string) (*Dispatch, error) {
	for _, d := range m.dispatches {
		if d.TripUUID != nil && *d.TripUUID == tripUUID {
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

func (m *mockRepo) HasActiveForOrder(_ context.Context, orderID int64) (bool, error) {
	for _, d := range m.dispatches {
		if d.OrderID == orderID && d.Status != "CANCELLED" {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockRepo) DriverHasActiveTrip(_ context.Context, userID int64, excludeDispatchID int64) (bool, error) {
	for _, d := range m.dispatches {
		if d.ID == excludeDispatchID || d.DriverUserID == nil || *d.DriverUserID != userID {
			continue
		}
		if IsActive(d.Status) {
			return true, nil
		}
	}
	return false, nil
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
	if d.Version != input.ExpectedVersion {
		return nil, ErrConflict
	}
	d.Status = input.Status
	d.Version++
	return d, nil
}

func (m *mockRepo) CompleteOrderIfReady(_ context.Context, orderID int64) error {
	if access, ok := m.orderAccess[orderID]; ok && (access.OrderStatus == "LOADED" || access.OrderStatus == "PARTIALLY_FULFILLED") {
		access.OrderStatus = "COMPLETED"
	}
	return nil
}

// SetDriverUser mirrors the repository's single conditional UPDATE.
func (m *mockRepo) SetDriverUser(ctx context.Context, dispatchID int64, userID int64, vehicleID int64) (*Dispatch, error) {
	d, ok := m.dispatches[dispatchID]
	if !ok || d.Status != "PENDING" || d.DriverUserID != nil || d.DriverID != nil {
		return nil, ErrTripTaken
	}
	if active, _ := m.DriverHasActiveTrip(ctx, userID, dispatchID); active {
		return nil, ErrActiveDriverTrip
	}
	for _, other := range m.dispatches {
		if other.ID != dispatchID && other.VehicleID != nil && *other.VehicleID == vehicleID &&
			(other.Status == "ACCEPTED" || other.Status == "DISPATCHED" || other.Status == "IN_TRANSIT") {
			return nil, ErrVehicleOnTrip
		}
	}
	d.DriverUserID = &userID
	d.VehicleID = &vehicleID
	d.Status = "ACCEPTED"
	return d, nil
}

func (m *mockRepo) OrderOnHold(_ context.Context, orderID int64) (bool, error) {
	return m.held[orderID], nil
}

func (m *mockRepo) DriverVehicles(_ context.Context, userID int64) ([]DriverVehicle, error) {
	return m.vehicles[userID], nil
}

func (m *mockRepo) CreateNotification(_ context.Context, userID int64, notifType, _, _ string, _ map[string]any) error {
	m.notified = append(m.notified, fmt.Sprintf("%d:%s", userID, notifType))
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

func (m *mockRepo) DriverEligibility(_ context.Context, userID int64) (DriverEligibility, error) {
	return m.drivers[userID], nil
}

// approveDriver registers an approved, active driver with one eligible TRUCK
// (vehicle id = 1000 + user id).
func (m *mockRepo) approveDriver(userID int64) {
	m.drivers[userID] = DriverEligibility{Found: true, ApprovalStatus: "APPROVED", Status: "ACTIVE"}
	m.vehicles[userID] = []DriverVehicle{truck(1000+userID, "")}
}

func truck(id int64, blocked string) DriverVehicle {
	return DriverVehicle{VehicleID: id, VehicleNumber: fmt.Sprintf("TS09T%d", id), VehicleType: strp("TRUCK"), BlockedReason: blocked}
}

func (m *mockRepo) RenewTripInvite(_ context.Context, dispatchID int64) (*Dispatch, error) {
	d, ok := m.dispatches[dispatchID]
	if !ok || d.Status != "PENDING" || d.DriverUserID != nil || d.IsOutsideTransport {
		return nil, ErrTripTaken
	}
	u, exp := "9b2d3c1e-1111-4a2b-8c3d-000000000001", time.Now().Add(72*time.Hour)
	d.TripUUID, d.InviteExpiresAt = &u, &exp
	return d, nil
}

func (m *mockRepo) GetOwnedNurseryID(_ context.Context, userID int64) (*int64, error) {
	id, ok := m.ownedNurseries[userID]
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func (m *mockRepo) MarkDelivered(_ context.Context, id int64, in MarkDeliveredInput) (*Dispatch, error) {
	d, ok := m.dispatches[id]
	if !ok || !onTheWay(d.Status) {
		return nil, ErrConflict
	}
	now := time.Now()
	d.Status, d.DeliveryDate = "DELIVERED", &now
	d.DeliveryConfirmation, d.DeliveryConfirmedByUserID = &in.Confirmation, &in.ByUserID
	d.DeliveryOverrideReason, d.DeliveryIssue = in.OverrideReason, in.Issue
	return d, nil
}

func (m *mockRepo) UseDeliveryCodeAttempt(_ context.Context, id int64, max int) (int, bool, error) {
	if m.dispatches[id].DeliveryCodeAttempts >= max {
		return max, false, nil
	}
	m.dispatches[id].DeliveryCodeAttempts++
	return m.dispatches[id].DeliveryCodeAttempts, true, nil
}

func (m *mockRepo) RecordReceipt(_ context.Context, id, userID int64, confirm bool, issue *string) error {
	d := m.dispatches[id]
	if confirm {
		c := ConfirmationApp
		d.DeliveryConfirmation, d.DeliveryConfirmedByUserID = &c, &userID
	}
	if d.DeliveryIssue == nil {
		d.DeliveryIssue = issue
	}
	return nil
}

func (m *mockRepo) GetUserNurseryIDs(_ context.Context, userID int64) ([]int64, error) {
	return m.userNurseries[userID], nil
}

// ── actor helpers ─────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func ownerBuyerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"BUYER", "NURSERY_OWNER"}}
}
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func driverActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }

func nid(n int64) *int64    { return &n }
func ptr64(n int64) *int64  { return &n }
func strp(s string) *string { return &s }

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

func TestCreate_OwnerWithLegacyBuyerRoleSuccess(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100)
	repo.seedOrderAccess(10, &nurseryID)
	_, err := svc(repo).Create(context.Background(), ownerBuyerActor(100), CreateDispatchRequest{OrderID: 10})
	if err != nil {
		t.Errorf("owner with legacy buyer role should create dispatch: %v", err)
	}
}

func TestCreate_ManagerSuccess(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100, 200)
	repo.seedOrderAccess(10, &nurseryID)
	_, err := svc(repo).Create(context.Background(), managerActor(200), CreateDispatchRequest{OrderID: 10})
	if err != nil {
		t.Errorf("manager create dispatch: %v", err)
	}
}

func TestCreate_BlocksOrderBeforeLoadingComplete(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100)
	repo.seedOrderAccessWithStatus(10, &nurseryID, "CONFIRMED")
	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateDispatchRequest{OrderID: 10})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("dispatch before loaded: want ErrInvalidStatus, got %v", err)
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

func TestCreate_DuplicateActiveOrderDispatch(t *testing.T) {
	repo := newMock()
	nurseryID := int64(1)
	repo.seedNursery(1, 100)
	repo.seedOrderAccess(10, &nurseryID)
	repo.seedDispatch(Dispatch{ID: 1, OrderID: 10, Status: "ACCEPTED"})

	_, err := svc(repo).Create(context.Background(), ownerActor(100), CreateDispatchRequest{OrderID: 10})
	if !errors.Is(err, ErrActiveDispatch) {
		t.Errorf("duplicate active order dispatch: want ErrActiveDispatch, got %v", err)
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

func TestUpdateStatus_DeliveredNeedsConfirmation(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, OrderID: 20, Status: "IN_TRANSIT", SellerNurseryID: &nid})
	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "DELIVERED"})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Errorf("PUT status DELIVERED: want ErrConfirmationRequired, got %v", err)
	}
}

func TestUpdateStatus_DeliveredRemovesLiveDriverLocation(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() {
		_ = client.Close()
		mr.Close()
	}()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("skipping redis-dependent test (sandbox or network restricted): %v", err)
	}

	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	driverUserID := int64(400)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid, DriverUserID: &driverUserID})

	liveGeo := redisgeo.New(client)
	if _, err := liveGeo.UpsertDriver(context.Background(), driverUserID, 17.385, 78.4867); err != nil {
		t.Fatalf("seed live driver location: %v", err)
	}

	reason := "Customer phone switched off"
	_, err := NewService(repo, nil, client).Deliver(context.Background(), ownerActor(100), 10, DeliverRequest{OverrideReason: &reason})
	if err != nil {
		t.Fatalf("deliver dispatch: %v", err)
	}

	loc, err := liveGeo.GetDriver(context.Background(), driverUserID)
	if err != nil {
		t.Fatalf("get live driver after delivery: %v", err)
	}
	if loc != nil {
		t.Fatalf("expected live location removed after delivery, got %#v", loc)
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
	_, err := svc(repo).AcceptDispatch(context.Background(), ownerActor(100), 10, testTrip, nil)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("owner accept dispatch: want ErrForbidden, got %v", err)
	}
}

func TestAcceptDispatch_ManagerForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), managerActor(200), 10, testTrip, nil)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("manager accept dispatch: want ErrForbidden, got %v", err)
	}
}

func TestAcceptDispatch_BuyerForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), buyerActor(300), 10, testTrip, nil)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("buyer accept dispatch: want ErrForbidden, got %v", err)
	}
}

func TestAcceptDispatch_DriverSuccess(t *testing.T) {
	repo := newMock()
	repo.approveDriver(400)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	d, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
	if err != nil {
		t.Fatalf("driver accept dispatch: %v", err)
	}
	if d.Status != "ACCEPTED" {
		t.Errorf("status after accept: want ACCEPTED, got %s", d.Status)
	}
}

func TestAcceptDispatch_NeedsTheTripCode(t *testing.T) {
	repo := newMock()
	repo.approveDriver(400)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, DispatchCode: "DSP-20261026-0001", Status: "PENDING", SellerNurseryID: &nid})
	for _, code := range []string{"", "DSP-20261026-0001", "10", "11111111-2222-3333-4444-555555555555"} {
		if _, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, code, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("code %q: want ErrNotFound, got %v", code, err)
		}
	}
	if _, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, "TRIP-"+strings.ToUpper(testTrip), nil); err != nil {
		t.Fatalf("the QR's TRIP-<uuid> joins: %v", err)
	}
}

func TestGetByCode_OnlyTheTripCodeFindsATrip(t *testing.T) {
	repo := newMock()
	repo.approveDriver(400)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, DispatchCode: "DSP-20261026-0001", Status: "PENDING", SellerNurseryID: &nid})
	if _, err := svc(repo).GetByCode(context.Background(), driverActor(400), "DSP-20261026-0001"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a guessable dispatch code must not find the trip, got %v", err)
	}
	if _, err := svc(repo).GetByCode(context.Background(), driverActor(400), "TRIP-"+testTrip); err != nil {
		t.Errorf("the QR code finds it: %v", err)
	}
}

func TestQR_AnyApprovedDriverTakesATripWithoutJoiningTheNursery(t *testing.T) {
	repo := newMock()
	repo.approveDriver(401) // no nursery link and no driver invite: just approved
	name, mobile := "Ravi", "9811300004"
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1),
		CustomerNameSnapshot: &name, CustomerMobileSnapshot: &mobile})
	d, err := svc(repo).GetByCode(context.Background(), driverActor(401), testTrip)
	if err != nil {
		t.Fatalf("approved driver previews the trip from its QR: %v", err)
	}
	if d.CustomerNameSnapshot != nil || d.CustomerMobileSnapshot != nil {
		t.Errorf("a driver deciding sees no customer contact, got %v %v", d.CustomerNameSnapshot, d.CustomerMobileSnapshot)
	}
	if d, err := svc(repo).AcceptDispatch(context.Background(), driverActor(401), 10, testTrip, nil); err != nil || d.Status != "ACCEPTED" {
		t.Fatalf("approved driver accepts: %v %v", d.Status, err)
	}
	// Scanning again is idempotent for the same driver, who now sees the customer.
	d, err = svc(repo).GetByCode(context.Background(), driverActor(401), testTrip)
	if err != nil || d.CustomerMobileSnapshot == nil {
		t.Errorf("the trip's driver sees whom to call: %v %v", d.CustomerMobileSnapshot, err)
	}
	if _, err := svc(repo).AcceptDispatch(context.Background(), driverActor(401), 10, testTrip, nil); err != nil {
		t.Errorf("repeat scan: %v", err)
	}
}

func TestAcceptDispatch_OnlyEligibleDrivers(t *testing.T) {
	approved := DriverEligibility{Found: true, ApprovalStatus: "APPROVED", Status: "ACTIVE"}
	auto := DriverVehicle{VehicleID: 7, VehicleNumber: "TS09A7", VehicleType: strp("AUTO")}
	lowerTruck := DriverVehicle{VehicleID: 8, VehicleNumber: "TS09T8", VehicleType: strp("truck")}
	cases := []struct {
		name     string
		driver   DriverEligibility
		vehicles []DriverVehicle
		need     *string
		want     error
	}{
		{"never applied", DriverEligibility{}, nil, nil, ErrDriverNotApproved},
		{"waiting for approval", DriverEligibility{Found: true, ApprovalStatus: "PENDING", Status: "ACTIVE"}, []DriverVehicle{truck(1, "")}, nil, ErrDriverNotApproved},
		{"suspended", DriverEligibility{Found: true, ApprovalStatus: "APPROVED", Status: "SUSPENDED"}, []DriverVehicle{truck(1, "")}, nil, ErrDriverNotApproved},
		{"stopped driving", DriverEligibility{Found: true, ApprovalStatus: "APPROVED", Status: "INACTIVE"}, []DriverVehicle{truck(1, "")}, nil, ErrDriverNotApproved},
		{"wrong lorry", approved, []DriverVehicle{auto}, strp("TRUCK"), ErrVehicleMismatch},
		{"the lorry asked for", approved, []DriverVehicle{lowerTruck}, strp("TRUCK"), nil},
		{"any lorry", approved, []DriverVehicle{auto}, nil, nil},
		{"no lorry at all", approved, nil, nil, ErrNoEligibleVehicle},
		{"link not approved yet", approved, []DriverVehicle{truck(1, "LINK_PENDING")}, nil, ErrNoEligibleVehicle},
		{"expired licence", approved, []DriverVehicle{truck(1, "LICENCE_EXPIRED")}, nil, ErrNoEligibleVehicle},
		{"lorry in maintenance", approved, []DriverVehicle{truck(1, "VEHICLE_MAINTENANCE")}, nil, ErrNoEligibleVehicle},
		{"two lorries, none chosen", approved, []DriverVehicle{truck(1, ""), truck(2, "")}, nil, ErrVehicleRequired},
		{"two lorries, one eligible", approved, []DriverVehicle{truck(1, "VEHICLE_ON_TRIP"), truck(2, "")}, nil, nil},
	}
	for _, c := range cases {
		repo := newMock()
		repo.drivers[400] = c.driver
		repo.vehicles[400] = c.vehicles
		repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1), RequiredVehicleType: c.need})
		_, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
		if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
		if c.want != nil && repo.dispatches[10].DriverUserID != nil {
			t.Errorf("%s: refused driver must not be assigned", c.name)
		}
	}
}

func TestAcceptDispatch_UnavailableTrips(t *testing.T) {
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	other := int64(402)
	cases := []struct {
		name string
		d    Dispatch
		want error
	}{
		{"cancelled", Dispatch{ID: 10, Status: "CANCELLED"}, ErrTripCancelled},
		{"taken", Dispatch{ID: 10, Status: "ACCEPTED", DriverUserID: &other}, ErrTripTaken},
		{"delivered", Dispatch{ID: 10, Status: "DELIVERED", DriverUserID: &other}, ErrTripTaken},
		{"QR expired", Dispatch{ID: 10, Status: "PENDING", InviteExpiresAt: &past}, ErrTripInviteExpired},
		{"outside lorry", Dispatch{ID: 10, Status: "PENDING", IsOutsideTransport: true}, ErrOutsideTransport},
		{"QR still valid", Dispatch{ID: 10, Status: "PENDING", InviteExpiresAt: &future}, nil},
	}
	for _, c := range cases {
		repo := newMock()
		repo.approveDriver(400)
		c.d.NurseryID = nid(1)
		repo.seedDispatch(c.d)
		_, previewErr := svc(repo).GetByCode(context.Background(), driverActor(400), testTrip)
		_, acceptErr := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
		for _, err := range []error{previewErr, acceptErr} {
			if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
				t.Errorf("%s: want %v, got %v", c.name, c.want, err)
			}
		}
	}
}

func TestAcceptDispatch_TwoDriversOneTrip(t *testing.T) {
	repo := newMock()
	repo.approveDriver(400)
	repo.approveDriver(401)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1), OwnerUserIDSnapshot: ptr64(100)})
	if _, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil); err != nil {
		t.Fatalf("first driver: %v", err)
	}
	if _, err := svc(repo).AcceptDispatch(context.Background(), driverActor(401), 10, testTrip, nil); !errors.Is(err, ErrTripTaken) {
		t.Errorf("second driver: want ErrTripTaken, got %v", err)
	}
	if got := *repo.dispatches[10].DriverUserID; got != 400 {
		t.Errorf("trip belongs to the first driver, got %d", got)
	}
	// The owner and the winning driver are told; nobody else.
	want := []string{"100:" + notifyqueue.EventDispatchAccepted, "400:" + notifyqueue.EventDispatchAccepted}
	if fmt.Sprint(repo.notified) != fmt.Sprint(want) {
		t.Errorf("notifications: got %v, want %v", repo.notified, want)
	}
}

func TestAcceptDispatch_LostRaceSaysTaken(t *testing.T) {
	// Both drivers passed the checks; the conditional UPDATE picks one.
	repo := newMock()
	repo.approveDriver(401)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1)})
	if _, err := repo.SetDriverUser(context.Background(), 10, 400, 1400); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetDriverUser(context.Background(), 10, 401, 1401); !errors.Is(err, ErrTripTaken) {
		t.Errorf("the loser of the race: want ErrTripTaken, got %v", err)
	}
	if len(repo.notified) != 0 {
		t.Errorf("no notification for a claim that failed: %v", repo.notified)
	}
}

func TestAcceptDispatch_ChosenLorry(t *testing.T) {
	approved := DriverEligibility{Found: true, ApprovalStatus: "APPROVED", Status: "ACTIVE"}
	auto := DriverVehicle{VehicleID: 3, VehicleNumber: "TS09A3", VehicleType: strp("AUTO")}
	cases := []struct {
		name   string
		chosen int64
		need   *string
		want   error
	}{
		{"an eligible lorry of mine", 2, nil, nil},
		{"someone else's lorry", 99, nil, ErrVehicleNotYours},
		{"my lorry under maintenance", 1, nil, VehicleBlockedError{Reason: "VEHICLE_MAINTENANCE"}},
		{"my lorry of the wrong type", 3, strp("TRUCK"), ErrVehicleMismatch},
	}
	for _, c := range cases {
		repo := newMock()
		repo.drivers[400] = approved
		repo.vehicles[400] = []DriverVehicle{truck(1, "VEHICLE_MAINTENANCE"), truck(2, ""), auto}
		repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1), RequiredVehicleType: c.need})
		got, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, &c.chosen)
		if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
			continue
		}
		if c.want == nil && (got.VehicleID == nil || *got.VehicleID != c.chosen) {
			t.Errorf("%s: trip must carry the chosen lorry, got %v", c.name, got.VehicleID)
		}
		if c.want != nil && repo.dispatches[10].DriverUserID != nil {
			t.Errorf("%s: refused driver must not be assigned", c.name)
		}
	}
}

func TestAcceptDispatch_OneLorryTwoDrivers(t *testing.T) {
	// Two drivers share lorry 5; it can only be on one trip at a time.
	repo := newMock()
	shared := truck(5, "")
	for _, uid := range []int64{400, 401} {
		repo.drivers[uid] = DriverEligibility{Found: true, ApprovalStatus: "APPROVED", Status: "ACTIVE"}
		repo.vehicles[uid] = []DriverVehicle{shared}
	}
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1)})
	repo.seedDispatch(Dispatch{ID: 11, Status: "PENDING", NurseryID: nid(1)})
	if _, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil); err != nil {
		t.Fatalf("first driver: %v", err)
	}
	// The second driver passed the checks before the first claim landed; the claim refuses.
	if _, err := repo.SetDriverUser(context.Background(), 11, 401, 5); !errors.Is(err, ErrVehicleOnTrip) {
		t.Errorf("second trip with the same lorry: want ErrVehicleOnTrip, got %v", err)
	}
}

func TestGetByCode_DriverSeesLorriesForTrip(t *testing.T) {
	repo := newMock()
	repo.drivers[400] = DriverEligibility{Found: true, ApprovalStatus: "APPROVED", Status: "ACTIVE"}
	repo.vehicles[400] = []DriverVehicle{truck(1, ""), truck(2, "LINK_PENDING"),
		{VehicleID: 3, VehicleNumber: "TS09A3", VehicleType: strp("AUTO")}}
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1), RequiredVehicleType: strp("TRUCK")})
	got, err := svc(repo).GetByCode(context.Background(), driverActor(400), testTrip)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{1: "", 2: "LINK_PENDING", 3: "VEHICLE_TYPE_MISMATCH"}
	if len(got.DriverVehicles) != 3 {
		t.Fatalf("want 3 lorries, got %+v", got.DriverVehicles)
	}
	for _, v := range got.DriverVehicles {
		if v.BlockedReason != want[v.VehicleID] || v.IsEligible != (want[v.VehicleID] == "") {
			t.Errorf("lorry %d: got reason %q eligible %v", v.VehicleID, v.BlockedReason, v.IsEligible)
		}
		if !v.IsEligible && v.BlockedMessage == "" {
			t.Errorf("lorry %d: a blocked lorry needs a message", v.VehicleID)
		}
	}
}

func TestRenewInvite(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	repo.approveDriver(400)
	past := time.Now().Add(-time.Minute)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", NurseryID: nid(1), SellerNurseryID: nid(1), InviteExpiresAt: &past})
	repo.seedOrderAccess(0, nid(1))
	expired := withLifecycle(ownerActor(100), *repo.dispatches[10])
	if expired.Capabilities.CanShareCode || !expired.Capabilities.CanRenewInvite {
		t.Errorf("expired QR: no share, offer a new one: %+v", expired.Capabilities)
	}
	if _, err := svc(repo).RenewInvite(context.Background(), driverActor(400), 10); !errors.Is(err, ErrForbidden) {
		t.Errorf("a driver cannot renew: %v", err)
	}
	d, err := svc(repo).RenewInvite(context.Background(), ownerActor(100), 10)
	if err != nil || d.TripUUID == nil || *d.TripUUID == testTrip {
		t.Fatalf("owner renews with a new QR: %v %v", d.TripUUID, err)
	}
	if _, err := svc(repo).GetByCode(context.Background(), driverActor(400), testTrip); !errors.Is(err, ErrNotFound) {
		t.Errorf("the old QR stops working: %v", err)
	}
	if _, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, *d.TripUUID, nil); err != nil {
		t.Errorf("the new QR works: %v", err)
	}
	if _, err := svc(repo).RenewInvite(context.Background(), ownerActor(100), 10); !errors.Is(err, ErrTripTaken) {
		t.Errorf("no new QR once a driver has it: %v", err)
	}
}

func TestAcceptDispatch_DriverWithActiveTripRejected(t *testing.T) {
	repo := newMock()
	repo.approveDriver(400)
	nid := int64(1)
	driverID := int64(400)
	repo.seedDispatch(Dispatch{ID: 9, Status: "IN_TRANSIT", SellerNurseryID: &nid, DriverUserID: &driverID})
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
	if !errors.Is(err, ErrActiveDriverTrip) {
		t.Errorf("driver with active trip accept: want ErrActiveDriverTrip, got %v", err)
	}
}

func TestAcceptDispatch_DriverWithTerminalTripCanAccept(t *testing.T) {
	repo := newMock()
	repo.approveDriver(400)
	nid := int64(1)
	driverID := int64(400)
	repo.seedDispatch(Dispatch{ID: 9, Status: "DELIVERED", SellerNurseryID: &nid, DriverUserID: &driverID})
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid})
	d, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
	if err != nil {
		t.Fatalf("driver with terminal trip should accept: %v", err)
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
	_, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
	if !errors.Is(err, ErrTripTaken) {
		t.Errorf("already accepted by other driver: want ErrTripTaken, got %v", err)
	}
}

func TestAcceptDispatch_NonPendingForbidden(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "IN_TRANSIT", SellerNurseryID: &nid})
	_, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
	if !errors.Is(err, ErrTripTaken) {
		t.Errorf("accept non-pending: want ErrTripTaken, got %v", err)
	}
}

func TestAcceptDispatch_IdempotentForSameDriver(t *testing.T) {
	repo := newMock()
	nid := int64(1)
	uid := int64(400)
	repo.seedDispatch(Dispatch{ID: 10, Status: "ACCEPTED", SellerNurseryID: &nid, DriverUserID: &uid})
	d, err := svc(repo).AcceptDispatch(context.Background(), driverActor(400), 10, testTrip, nil)
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
	d, err := svc(repo).GetByCode(context.Background(), adminActor(1), testTrip)
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if d.ID != 10 {
		t.Errorf("id: want 10, got %d", d.ID)
	}
}

func TestGetByCode_OnlyDriversPreviewUnclaimedTrips(t *testing.T) {
	repo := newMock()
	repo.approveDriver(5)
	repo.seedDispatch(Dispatch{ID: 10, DispatchCode: "DC-1", Status: "PENDING", NurseryID: nid(1)})
	if _, err := svc(repo).GetByCode(context.Background(), driverActor(5), testTrip); err != nil {
		t.Errorf("driver preview of a pending trip: %v", err)
	}
	if _, err := svc(repo).GetByCode(context.Background(), buyerActor(6), testTrip); !errors.Is(err, ErrForbidden) {
		t.Errorf("buyer lookup: want ErrForbidden, got %v", err)
	}
	mine, other := int64(7), "6fa459ea-ee8a-3ca4-894e-db77e160355e"
	repo.seedDispatch(Dispatch{ID: 11, DispatchCode: "DC-2", Status: "ACCEPTED", DriverUserID: &mine, TripUUID: &other})
	if _, err := svc(repo).GetByCode(context.Background(), driverActor(5), other); !errors.Is(err, ErrTripTaken) {
		t.Errorf("another driver's trip: want ErrTripTaken, got %v", err)
	}
}

func TestUpdateStatus_BuyerAndDriverLimits(t *testing.T) {
	repo := newMock()
	drv := int64(7)
	repo.seedDispatch(Dispatch{ID: 10, Status: "ACCEPTED", DriverUserID: &drv})
	if _, err := svc(repo).UpdateStatus(context.Background(), driverActor(7), 10, UpdateStatusRequest{Status: "CANCELLED"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("driver cancel: want ErrForbidden, got %v", err)
	}
	if _, err := svc(repo).UpdateStatus(context.Background(), driverActor(7), 10, UpdateStatusRequest{Status: "DISPATCHED"}); err != nil {
		t.Errorf("driver dispatch: %v", err)
	}
}

func TestUpdateStatus_DriverStartsStraightFromAccepted(t *testing.T) {
	repo := newMock()
	drv := int64(7)
	repo.seedDispatch(Dispatch{ID: 10, Status: "ACCEPTED", DriverUserID: &drv})
	d, err := svc(repo).UpdateStatus(context.Background(), driverActor(7), 10, UpdateStatusRequest{Status: "IN_TRANSIT"})
	if err != nil || d.Status != "IN_TRANSIT" {
		t.Fatalf("driver start from ACCEPTED: %v %v", d.Status, err)
	}
	if !BuildCapabilities(driverActor(7), "ACCEPTED", false).CanStartTrip {
		t.Error("driver should see Start Trip on an accepted trip")
	}
}

func TestMarkDispatched_OfferedOnlyForOutsideLorries(t *testing.T) {
	app := withLifecycle(ownerActor(100), Dispatch{ID: 1, Status: "ACCEPTED"})
	if app.Capabilities.CanMarkDispatched {
		t.Error("app-driver trip: the driver starts it, no Mark Dispatched")
	}
	lorry := withLifecycle(ownerActor(100), Dispatch{ID: 2, Status: "PENDING", IsOutsideTransport: true})
	if !lorry.Capabilities.CanMarkDispatched {
		t.Error("outside lorry: the nursery marks it dispatched")
	}
}

func TestGetByCode_NotFound(t *testing.T) {
	repo := newMock()
	_, err := svc(repo).GetByCode(context.Background(), driverActor(5), "BOGUS")
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
		{"PENDING", "IN_TRANSIT", false},
		{"PENDING", "CANCELLED", true},
		{"PENDING", "DELIVERED", false},
		{"ACCEPTED", "DISPATCHED", true},
		{"ACCEPTED", "IN_TRANSIT", true}, // driver starts without a hand-over
		{"ACCEPTED", "CANCELLED", true},
		{"DISPATCHED", "IN_TRANSIT", true},
		{"DISPATCHED", "CANCELLED", false},
		{"DISPATCHED", "DELIVERED", false},
		{"IN_TRANSIT", "DELIVERED", true},
		{"IN_TRANSIT", "CANCELLED", false},
		{"IN_TRANSIT", "PENDING", false},
		{"DELIVERED", "IN_TRANSIT", false},
		{"DELIVERED", "CANCELLED", false},
		{"CANCELLED", "PENDING", false},
	}
	for _, c := range cases {
		got := CanTransition(c.from, c.to)
		if got != c.valid {
			t.Errorf("transition %s→%s: want valid=%v, got %v", c.from, c.to, c.valid, got)
		}
	}
}

func TestIsAllowedStatus(t *testing.T) {
	allowed := []string{"PENDING", "ACCEPTED", "DISPATCHED", "IN_TRANSIT", "DELIVERED", "CANCELLED"}
	notAllowed := []string{"LOADING", "COMPLETED", "UNICORN", ""}
	for _, s := range allowed {
		if !AllowedStatus(s) {
			t.Errorf("AllowedStatus(%s): want true", s)
		}
	}
	for _, s := range notAllowed {
		if AllowedStatus(s) {
			t.Errorf("AllowedStatus(%s): want false", s)
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

func TestUpdateStatus_StaleIfMatchConflicts(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, 100)
	nid := int64(1)
	repo.seedDispatch(Dispatch{ID: 10, Status: "PENDING", SellerNurseryID: &nid, Version: 2})

	if _, err := svc(repo).UpdateStatus(rowversion.With(context.Background(), 1), ownerActor(100), 10, UpdateStatusRequest{Status: "DISPATCHED"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale If-Match: want ErrConflict, got %v", err)
	}
	if repo.dispatches[10].Status != "PENDING" {
		t.Fatal("a conflicting write must not change the dispatch")
	}
	d, err := svc(repo).UpdateStatus(rowversion.With(context.Background(), 2), ownerActor(100), 10, UpdateStatusRequest{Status: "DISPATCHED"})
	if err != nil || d.Status != "DISPATCHED" || d.Version != 3 {
		t.Fatalf("matching If-Match: got %+v, %v", d, err)
	}
}

// ── Delivery confirmation ─────────────────────────────────────────────────────

func deliveryWorld(status string) (*mockRepo, *Service, Dispatch) {
	repo := newMock()
	repo.seedNursery(1, 100)
	n, buyer, driver, trip := int64(1), int64(300), int64(400), "6f1c2a7e-0000-4000-8000-000000000001"
	repo.seedOrderAccess(20, &n)
	d := Dispatch{ID: 10, OrderID: 20, Status: status, SellerNurseryID: &n, CustomerUserID: &buyer, DriverUserID: &driver, TripUUID: &trip}
	repo.seedDispatch(d)
	s := svc(repo)
	s.SetDeliveryCodeKey([]byte("test-secret"))
	return repo, s, d
}

func TestDeliveryCode_StableSecretAndPerTrip(t *testing.T) {
	_, s, d := deliveryWorld("IN_TRANSIT")
	a, ok := s.deliveryCode(d)
	b, _ := s.deliveryCode(d)
	if !ok || len(a) != 4 || a != b {
		t.Fatalf("code %q / %q, ok=%v: want the same 4 digits every time", a, b, ok)
	}
	other := "6f1c2a7e-0000-4000-8000-000000000002"
	d2 := d
	d2.TripUUID = &other
	c, _ := s.deliveryCode(d2)
	s.SetDeliveryCodeKey([]byte("another-secret"))
	e, _ := s.deliveryCode(d)
	if c == a && e == a {
		t.Fatalf("code does not depend on the trip or the secret")
	}
	s.SetDeliveryCodeKey(nil)
	if _, ok := s.deliveryCode(d); ok {
		t.Fatal("no secret must mean no code")
	}
}

func TestPresent_CodeOnlyForTheCustomerOnTheWay(t *testing.T) {
	_, s, d := deliveryWorld("IN_TRANSIT")
	if s.present(buyerActor(300), d).DeliveryCode == nil {
		t.Fatal("customer must see the code while the trip is on its way")
	}
	for _, a := range []ActorContext{ownerActor(100), driverActor(400), buyerActor(999), adminActor(1)} {
		if s.present(a, d).DeliveryCode != nil {
			t.Fatalf("%v must never see the code", a.Roles)
		}
	}
	d.Status = "PENDING"
	if s.present(buyerActor(300), d).DeliveryCode != nil {
		t.Fatal("no code before the trip is dispatched")
	}
}

func TestDeliver_WithCustomerCode(t *testing.T) {
	repo, s, d := deliveryWorld("DISPATCHED") // the lorry may arrive before Start
	code, _ := s.deliveryCode(d)
	got, err := s.Deliver(context.Background(), driverActor(400), 10, DeliverRequest{Code: code})
	if err != nil || got.Status != "DELIVERED" || *got.DeliveryConfirmation != ConfirmationCode {
		t.Fatalf("deliver with code: %+v %v", got.DeliveryConfirmation, err)
	}
	if repo.orderAccess[20].OrderStatus != "COMPLETED" {
		t.Errorf("order not completed")
	}
}

func TestDeliver_WrongCodesLockAfterFive(t *testing.T) {
	repo, s, d := deliveryWorld("IN_TRANSIT")
	code, _ := s.deliveryCode(d)
	wrong := "0000"
	if code == wrong {
		wrong = "1111"
	}
	for i := 1; i <= 4; i++ {
		if _, err := s.Deliver(context.Background(), driverActor(400), 10, DeliverRequest{Code: wrong}); !errors.Is(err, ErrWrongDeliveryCode) {
			t.Fatalf("try %d: want ErrWrongDeliveryCode, got %v", i, err)
		}
	}
	if _, err := s.Deliver(context.Background(), driverActor(400), 10, DeliverRequest{Code: wrong}); !errors.Is(err, ErrDeliveryCodeLocked) {
		t.Fatalf("5th wrong: want locked, got %v", err)
	}
	if _, err := s.Deliver(context.Background(), driverActor(400), 10, DeliverRequest{Code: code}); !errors.Is(err, ErrDeliveryCodeLocked) {
		t.Fatalf("right code after lock: want locked, got %v", err)
	}
	if s.present(buyerActor(300), *repo.dispatches[10]).DeliveryCode != nil {
		t.Fatal("a locked code must not be shown")
	}
}

func TestDeliver_OverrideOnlyStaffWithReason(t *testing.T) {
	_, s, _ := deliveryWorld("IN_TRANSIT")
	short, reason := "no", "Customer not reachable, plants left with the watchman"
	if _, err := s.Deliver(context.Background(), driverActor(400), 10, DeliverRequest{OverrideReason: &reason}); !errors.Is(err, ErrDeliveryCodeRequired) {
		t.Fatalf("driver override: want ErrDeliveryCodeRequired, got %v", err)
	}
	if _, err := s.Deliver(context.Background(), ownerActor(100), 10, DeliverRequest{OverrideReason: &short}); !errors.Is(err, ErrOverrideReason) {
		t.Fatalf("short reason: want ErrOverrideReason, got %v", err)
	}
	if _, err := s.Deliver(context.Background(), buyerActor(300), 10, DeliverRequest{OverrideReason: &reason}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("customer cannot use deliver: got %v", err)
	}
	got, err := s.Deliver(context.Background(), ownerActor(100), 10, DeliverRequest{OverrideReason: &reason})
	if err != nil || *got.DeliveryConfirmation != ConfirmationUnconfirmed {
		t.Fatalf("owner override: %v %v", got.DeliveryConfirmation, err)
	}
}

func TestReceipt_CustomerConfirmsAndReports(t *testing.T) {
	repo, s, _ := deliveryWorld("IN_TRANSIT")
	if _, err := s.ConfirmReceipt(context.Background(), ownerActor(100), 10, ReceiptRequest{Consent: buyerOK}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("only the customer: got %v", err)
	}
	if _, err := s.ConfirmReceipt(context.Background(), buyerActor(300), 10, ReceiptRequest{}); !errors.Is(err, consent.ErrRequired) {
		t.Fatalf("confirming without the buyer's acknowledgement: want consent.ErrRequired, got %v", err)
	}
	got, err := s.ConfirmReceipt(context.Background(), buyerActor(300), 10, ReceiptRequest{Consent: buyerOK})
	if err != nil || got.Status != "DELIVERED" || *got.DeliveryConfirmation != ConfirmationApp {
		t.Fatalf("received in app: %v", err)
	}
	if !got.Capabilities.CanReportIssue || got.Capabilities.CanConfirmReceipt {
		t.Fatalf("after confirming: report yes, confirm no: %+v", got.Capabilities)
	}
	if _, err := s.ConfirmReceipt(context.Background(), buyerActor(300), 10, ReceiptRequest{Consent: buyerOK}); !errors.Is(err, ErrAlreadyConfirmed) {
		t.Fatalf("confirm twice: got %v", err)
	}
	issue := "10 plants broken"
	if got, err = s.ConfirmReceipt(context.Background(), buyerActor(300), 10, ReceiptRequest{Issue: &issue}); err != nil || *got.DeliveryIssue != issue {
		t.Fatalf("report problem: %v", err)
	}
	old := time.Now().Add(-49 * time.Hour)
	repo.dispatches[10].DeliveryDate = &old
	if _, err := s.ConfirmReceipt(context.Background(), buyerActor(300), 10, ReceiptRequest{Issue: &issue}); !errors.Is(err, ErrReceiptWindowClosed) {
		t.Fatalf("after 48h: got %v", err)
	}
}

func TestReceipt_ConfirmsAnOverride(t *testing.T) {
	_, s, _ := deliveryWorld("IN_TRANSIT")
	reason := "Customer phone switched off at delivery"
	if _, err := s.Deliver(context.Background(), ownerActor(100), 10, DeliverRequest{OverrideReason: &reason}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmReceipt(context.Background(), buyerActor(300), 10, ReceiptRequest{}); !errors.Is(err, consent.ErrRequired) {
		t.Fatalf("confirming without the buyer's acknowledgement: want consent.ErrRequired, got %v", err)
	}
	got, err := s.ConfirmReceipt(context.Background(), buyerActor(300), 10, ReceiptRequest{Consent: buyerOK})
	if err != nil || *got.DeliveryConfirmation != ConfirmationApp {
		t.Fatalf("customer confirms the override later: %v", err)
	}
}

func TestOutsideTransport_NoDriverJoin(t *testing.T) {
	repo, s, d := deliveryWorld("PENDING")
	repo.dispatches[10].IsOutsideTransport = true
	if _, err := s.AcceptDispatch(context.Background(), driverActor(401), 10, *repo.dispatches[10].TripUUID, nil); !errors.Is(err, ErrOutsideTransport) {
		t.Fatalf("join outside trip: got %v", err)
	}
	if caps := s.present(ownerActor(100), *repo.dispatches[10]).Capabilities; caps.CanShareCode || caps.CanAccept {
		t.Fatalf("outside trip offers QR/accept: %+v", caps)
	}
	_ = d
	if _, err := normalizeCreate(CreateDispatchRequest{OrderID: 20, IsOutsideTransport: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("outside without vehicle: got %v", err)
	}
	v, m := " ap05 tb 1234 ", "+91 98765 43210"
	in, err := normalizeCreate(CreateDispatchRequest{OrderID: 20, IsOutsideTransport: true, OutsideVehicleNumber: &v, OutsideDriverMobile: &m})
	if err != nil || *in.OutsideVehicleNumber != "AP05 TB 1234" || *in.OutsideDriverMobile != "9876543210" {
		t.Fatalf("normalize outside: %+v %v", in, err)
	}
}

type recordingEvents struct{ events []messaging.Event }

func (r *recordingEvents) NotifyAsync(ev messaging.Event) { r.events = append(r.events, ev) }

// A nursery override must reach the customer by SMS/email too: the 48-hour
// dispute window protects only a customer who hears about it.
func TestDeliver_OverrideTellsTheCustomerBySMS(t *testing.T) {
	_, s, _ := deliveryWorld("IN_TRANSIT")
	ev := &recordingEvents{}
	s.SetEvents(ev)
	reason := "Customer not reachable, plants left with the watchman"
	if _, err := s.Deliver(context.Background(), ownerActor(100), 10, DeliverRequest{OverrideReason: &reason}); err != nil {
		t.Fatal(err)
	}
	if len(ev.events) != 1 || ev.events[0].Code != sms.TemplateDeliveryUnconfirmed || ev.events[0].UserID != 300 ||
		ev.events[0].Values["reason"] != reason {
		t.Fatalf("events: %+v", ev.events)
	}
	if _, ok := ev.events[0].Values["dispatch_code"]; !ok {
		t.Fatal("the SMS needs the dispatch code")
	}

	// Delivered with the customer's own code: nothing to dispute, no SMS.
	_, s2, d := deliveryWorld("IN_TRANSIT")
	ev2 := &recordingEvents{}
	s2.SetEvents(ev2)
	code, _ := s2.deliveryCode(d)
	if _, err := s2.Deliver(context.Background(), driverActor(400), 10, DeliverRequest{Code: code}); err != nil {
		t.Fatal(err)
	}
	if len(ev2.events) != 0 {
		t.Fatalf("code delivery raised %+v", ev2.events)
	}
}

// The customer who is not on the app gets the delivery code by SMS when the
// truck leaves; the event is masked in logs and skipped for app users.
func TestOnTheWay_SendsTheCodeBySMS(t *testing.T) {
	repo, s, _ := deliveryWorld("ACCEPTED")
	ev := &recordingEvents{}
	s.SetEvents(ev)
	if _, err := s.UpdateStatus(context.Background(), driverActor(400), 10, UpdateStatusRequest{Status: "IN_TRANSIT"}); err != nil {
		t.Fatal(err)
	}
	code, _ := s.deliveryCode(*repo.dispatches[10])
	if len(ev.events) != 1 || ev.events[0].Code != sms.TemplateDeliveryOnTheWay || ev.events[0].UserID != 300 ||
		!ev.events[0].NotOnAppOnly || ev.events[0].Values["delivery_code"] != code || ev.events[0].Sensitive[0] != "delivery_code" {
		t.Fatalf("events: %+v", ev.events)
	}
}

func TestCancel_TellsTheDriver(t *testing.T) {
	repo, s, _ := deliveryWorld("ACCEPTED")
	if _, err := s.UpdateStatus(context.Background(), ownerActor(100), 10, UpdateStatusRequest{Status: "CANCELLED"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(repo.notified, "400:TRIP_CANCELLED") {
		t.Fatalf("driver not told: %v", repo.notified)
	}
}

var (
	buyerOK  = &consent.Given{Version: consent.Templates[consent.BuyerDelivery].Version, Accepted: true}
	driverOK = &consent.Given{Version: consent.Templates[consent.DriverDispatch].Version, Accepted: true}
)

func TestStartTrip_DriverConfirmsFirst(t *testing.T) {
	for _, c := range []struct {
		name     string
		requires bool
		given    *consent.Given
		ok       bool
	}{
		{"a new trip without the driver's confirmation", true, nil, false},
		{"a new trip, confirmed", true, driverOK, true},
		{"a trip made before confirmations existed", false, nil, true},
	} {
		repo := newMock()
		repo.seedNursery(1, 100)
		uid := int64(400)
		repo.seedDispatch(Dispatch{ID: 10, Status: "ACCEPTED", NurseryID: nid(1), SellerNurseryID: nid(1), DriverUserID: &uid, RequiresConsent: c.requires})
		repo.seedOrderAccess(0, nid(1))
		_, err := svc(repo).UpdateStatus(context.Background(), driverActor(400), 10, UpdateStatusRequest{Status: "DISPATCHED", Consent: c.given})
		if c.ok != (err == nil) {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
}
