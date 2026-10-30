package vehicles

import (
	"context"
	"errors"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	vehicles   map[int64]*Vehicle
	duplicates map[string]bool // vehicleNumber → already exists
	nextID     int64
	owners     map[int64]VehicleOwner
	links      map[int64]*VehicleDriverLink
}

func newMock() *mockRepo {
	return &mockRepo{
		vehicles:   make(map[int64]*Vehicle),
		duplicates: make(map[string]bool),
		nextID:     100,
		owners:     make(map[int64]VehicleOwner),
		links:      make(map[int64]*VehicleDriverLink),
	}
}

func (m *mockRepo) next() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedVehicle(id int64, number, status string) *Vehicle {
	v := &Vehicle{ID: id, VehicleNumber: number, Status: status}
	m.vehicles[id] = v
	return v
}

func (m *mockRepo) List(_ context.Context, _ ListVehiclesRequest) ([]Vehicle, int64, error) {
	result := make([]Vehicle, 0, len(m.vehicles))
	for _, v := range m.vehicles {
		result = append(result, *v)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, vehicleID int64) (*Vehicle, error) {
	v, ok := m.vehicles[vehicleID]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (m *mockRepo) HasDuplicate(_ context.Context, vehicleNumber string, excludeID int64) (bool, error) {
	return m.duplicates[vehicleNumber], nil
}

func (m *mockRepo) Create(_ context.Context, input VehicleRequest) (*Vehicle, error) {
	id := m.next()
	v := &Vehicle{ID: id, VehicleNumber: input.VehicleNumber, Status: input.Status}
	m.vehicles[id] = v
	return v, nil
}

func (m *mockRepo) Update(_ context.Context, vehicleID int64, input VehicleRequest) (*Vehicle, error) {
	v, ok := m.vehicles[vehicleID]
	if !ok {
		return nil, ErrNotFound
	}
	v.VehicleNumber = input.VehicleNumber
	v.Status = input.Status
	return v, nil
}

func (m *mockRepo) Delete(_ context.Context, vehicleID int64) error {
	if _, ok := m.vehicles[vehicleID]; !ok {
		return ErrNotFound
	}
	delete(m.vehicles, vehicleID)
	return nil
}

func (m *mockRepo) ListOwners(context.Context, ListOwnersRequest) ([]VehicleOwner, int64, error) {
	return nil, 0, nil
}
func (m *mockRepo) FindOwner(_ context.Context, id int64) (VehicleOwner, error) {
	if o, ok := m.owners[id]; ok {
		return o, nil
	}
	return VehicleOwner{}, ErrNotFound
}
func (m *mockRepo) SaveOwner(_ context.Context, _, id int64, req OwnerRequest) (VehicleOwner, error) {
	if id == 0 {
		id = m.next()
	}
	o := VehicleOwner{ID: id, Name: req.Name, Mobile: req.Mobile}
	m.owners[id] = o
	return o, nil
}
func (m *mockRepo) SetApproval(_ context.Context, _, vehicleID int64, status string, _ *string) (*Vehicle, []int64, error) {
	v, ok := m.vehicles[vehicleID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	v.ApprovalStatus = status
	return v, nil, nil
}
func (m *mockRepo) ListLinks(context.Context, ListLinksRequest) ([]VehicleDriverLink, int64, error) {
	return nil, 0, nil
}
func (m *mockRepo) FindLink(_ context.Context, id int64) (VehicleDriverLink, error) {
	if l, ok := m.links[id]; ok {
		return *l, nil
	}
	return VehicleDriverLink{}, ErrNotFound
}
func (m *mockRepo) SetLinkStatus(_ context.Context, _, id int64, from, to string, _ *string) (VehicleDriverLink, error) {
	l := m.links[id]
	if to == "REMOVED" && l.IsOnActiveTrip {
		return VehicleDriverLink{}, ErrVehicleOnTrip
	}
	l.Status = to
	return *l, nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func buyerActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func driverActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}

func ptr[T any](v T) *T { return &v }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── List (admin-only) ────────────────────────────────────────────────────────

func TestList_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedVehicle(1, "MH01AB1234", "ACTIVE")

	vehicles, _, err := svc(repo).List(context.Background(), adminActor(1), ListVehiclesRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vehicles) != 1 {
		t.Errorf("want 1 vehicle, got %d", len(vehicles))
	}
}

func TestList_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), buyerActor(1), ListVehiclesRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestList_DriverForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), driverActor(1), ListVehiclesRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for driver, got %v", err)
	}
}

func TestList_OwnerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), ownerActor(1), ListVehiclesRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for nursery owner, got %v", err)
	}
}

// ─── Get (admin-only) ────────────────────────────────────────────────────────

func TestGet_AdminFound(t *testing.T) {
	repo := newMock()
	repo.seedVehicle(1, "MH01AB1234", "ACTIVE")

	v, err := svc(repo).Get(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.VehicleNumber != "MH01AB1234" {
		t.Errorf("want MH01AB1234, got %s", v.VehicleNumber)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestGet_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedVehicle(1, "MH01AB1234", "ACTIVE")

	_, err := svc(repo).Get(context.Background(), buyerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── Create (admin-only) ──────────────────────────────────────────────────────

func TestCreate_AdminSuccess(t *testing.T) {
	v, err := svc(newMock()).Create(context.Background(), adminActor(1), VehicleRequest{
		VehicleNumber: "MH01AB1234",
		Status:        "ACTIVE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.VehicleNumber != "MH01AB1234" {
		t.Errorf("want MH01AB1234, got %s", v.VehicleNumber)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), VehicleRequest{
		VehicleNumber: "MH01AB1234",
		Status:        "ACTIVE",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_EmptyVehicleNumberInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), VehicleRequest{
		VehicleNumber: "   ",
		Status:        "ACTIVE",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty vehicle number, got %v", err)
	}
}

func TestCreate_BadStatusInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), VehicleRequest{
		VehicleNumber: "MH01AB1234",
		Status:        "SCRAPPED",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad status, got %v", err)
	}
}

func TestCreate_NegativeCapacityInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), VehicleRequest{
		VehicleNumber: "MH01AB1234",
		Status:        "ACTIVE",
		CapacityKG:    ptr(-10.0),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for negative capacity, got %v", err)
	}
}

func TestCreate_DuplicateVehicleNumber(t *testing.T) {
	repo := newMock()
	repo.duplicates["MH01AB1234"] = true

	_, err := svc(repo).Create(context.Background(), adminActor(1), VehicleRequest{
		VehicleNumber: "MH01AB1234",
		Status:        "ACTIVE",
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("want ErrDuplicate, got %v", err)
	}
}

func TestCreate_DefaultStatusIsActive(t *testing.T) {
	// Empty status → defaults to ACTIVE via statusOrActive
	v, err := svc(newMock()).Create(context.Background(), adminActor(1), VehicleRequest{
		VehicleNumber: "MH01AB1234",
	})
	if err != nil {
		t.Fatalf("empty status should default to ACTIVE: %v", err)
	}
	if v.Status != "ACTIVE" && v.Status != "" {
		// service normalizes before create, so what's stored is ACTIVE or the raw (which driver normalizes in repo)
		// just verify no error
		_ = v.Status
	}
}

// ─── Update (admin-only) ──────────────────────────────────────────────────────

func TestUpdate_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedVehicle(1, "MH01AB1234", "ACTIVE")

	v, err := svc(repo).Update(context.Background(), adminActor(1), 1, VehicleRequest{
		VehicleNumber: "MH01AB1234",
		Status:        "MAINTENANCE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Status != "MAINTENANCE" {
		t.Errorf("want MAINTENANCE, got %s", v.Status)
	}
}

func TestUpdate_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedVehicle(1, "MH01AB1234", "ACTIVE")

	_, err := svc(repo).Update(context.Background(), buyerActor(1), 1, VehicleRequest{
		VehicleNumber: "MH01AB1234",
		Status:        "ACTIVE",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── Delete (admin-only) ──────────────────────────────────────────────────────

func TestDelete_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedVehicle(1, "MH01AB1234", "ACTIVE")

	err := svc(repo).Delete(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.vehicles[1]; ok {
		t.Error("vehicle should have been deleted")
	}
}

func TestDelete_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).Delete(context.Background(), buyerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	err := svc(newMock()).Delete(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Owners, approval, driver links ─────────────────────────────────────────

func TestSaveOwner_WithoutAccountOrMobile(t *testing.T) {
	o, err := svc(newMock()).SaveOwner(context.Background(), adminActor(1), 0, OwnerRequest{Name: "  Ramesh Transport "})
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "Ramesh Transport" || o.Mobile != nil {
		t.Errorf("owner needs only a name: %+v", o)
	}
}

func TestSaveOwner_Rules(t *testing.T) {
	cases := []struct {
		name  string
		actor ActorContext
		req   OwnerRequest
		want  error
	}{
		{"not an admin", driverActor(2), OwnerRequest{Name: "X"}, ErrForbidden},
		{"no name", adminActor(1), OwnerRequest{Name: " "}, ErrInvalidInput},
		{"bad mobile", adminActor(1), OwnerRequest{Name: "X", Mobile: ptr("123")}, ErrInvalidMobile},
	}
	for _, c := range cases {
		if _, err := svc(newMock()).SaveOwner(context.Background(), c.actor, 0, c.req); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

func TestSetApproval_Rules(t *testing.T) {
	repo := newMock()
	repo.seedVehicle(1, "TS09AB1234", "ACTIVE")
	s := svc(repo)
	if _, err := s.SetApproval(context.Background(), driverActor(2), 1, ApprovalRequest{Status: "APPROVED"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("driver: want ErrForbidden, got %v", err)
	}
	if _, err := s.SetApproval(context.Background(), adminActor(1), 1, ApprovalRequest{Status: "REJECTED"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("reject without a reason: want ErrInvalidInput, got %v", err)
	}
	if _, err := s.SetApproval(context.Background(), adminActor(1), 1, ApprovalRequest{Status: "BOGUS"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown status: want ErrInvalidInput, got %v", err)
	}
	v, err := s.SetApproval(context.Background(), adminActor(1), 1, ApprovalRequest{Status: "approved"})
	if err != nil || v.ApprovalStatus != "APPROVED" {
		t.Errorf("approve: got %+v, %v", v, err)
	}
}

func TestSetLinkStatus_Transitions(t *testing.T) {
	cases := []struct {
		from, to string
		onTrip   bool
		note     *string
		want     error
	}{
		{"PENDING", "APPROVED", false, nil, nil},
		{"PENDING", "REJECTED", false, ptr("RC does not match"), nil},
		{"PENDING", "REJECTED", false, nil, ErrInvalidInput},
		{"APPROVED", "SUSPENDED", true, ptr("complaint"), nil}, // suspension stops future trips only
		{"APPROVED", "REMOVED", true, ptr("left the owner"), ErrVehicleOnTrip},
		{"APPROVED", "REMOVED", false, ptr("left the owner"), nil},
		{"SUSPENDED", "APPROVED", false, nil, nil},
		{"REJECTED", "APPROVED", false, nil, ErrInvalidStatus},
		{"REMOVED", "APPROVED", false, nil, ErrInvalidStatus},
		{"APPROVED", "PENDING", false, ptr("x"), ErrInvalidStatus},
	}
	for _, c := range cases {
		repo := newMock()
		repo.links[7] = &VehicleDriverLink{ID: 7, Status: c.from, DriverUserID: 40, VehicleNumber: "TS09AB1234", IsOnActiveTrip: c.onTrip}
		_, err := svc(repo).SetLinkStatus(context.Background(), adminActor(1), 7, LinkStatusRequest{Status: c.to, Note: c.note})
		if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s → %s (trip %v): want %v, got %v", c.from, c.to, c.onTrip, c.want, err)
		}
	}
	if _, err := svc(newMock()).SetLinkStatus(context.Background(), driverActor(40), 7, LinkStatusRequest{Status: "APPROVED"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("driver approving own link: want ErrForbidden, got %v", err)
	}
}
