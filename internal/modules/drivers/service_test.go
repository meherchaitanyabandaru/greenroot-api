package drivers

import (
	"context"
	"errors"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	drivers    map[int64]*Driver       // driverID → Driver
	byUserID   map[int64]*Driver       // userID → Driver
	owners     map[int64]bool          // userID → owns a nursery
	duplicates map[string]bool         // licenseNumber → exists
	nextID     int64
}

func newMock() *mockRepo {
	return &mockRepo{
		drivers:    make(map[int64]*Driver),
		byUserID:   make(map[int64]*Driver),
		owners:     make(map[int64]bool),
		duplicates: make(map[string]bool),
		nextID:     100,
	}
}

func (m *mockRepo) next() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedDriver(id int64, userID *int64, status string) *Driver {
	d := &Driver{ID: id, UserID: userID, Status: status, ApprovalStatus: "PENDING"}
	m.drivers[id] = d
	if userID != nil {
		m.byUserID[*userID] = d
	}
	return d
}

func (m *mockRepo) List(_ context.Context, _ ListDriversRequest) ([]Driver, int64, error) {
	result := make([]Driver, 0, len(m.drivers))
	for _, d := range m.drivers {
		result = append(result, *d)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, driverID int64) (*Driver, error) {
	d, ok := m.drivers[driverID]
	if !ok {
		return nil, ErrNotFound
	}
	return d, nil
}

func (m *mockRepo) FindByUserID(_ context.Context, userID int64) (*Driver, error) {
	d, ok := m.byUserID[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return d, nil
}

func (m *mockRepo) HasDuplicate(_ context.Context, input DriverInput, excludeDriverID int64) (bool, error) {
	if input.LicenseNumber == nil {
		return false, nil
	}
	return m.duplicates[*input.LicenseNumber], nil
}

func (m *mockRepo) Create(_ context.Context, input DriverInput) (*Driver, error) {
	id := m.next()
	d := &Driver{ID: id, UserID: input.UserID, Status: input.Status, ApprovalStatus: "PENDING"}
	m.drivers[id] = d
	if input.UserID != nil {
		m.byUserID[*input.UserID] = d
	}
	return d, nil
}

func (m *mockRepo) Update(_ context.Context, driverID int64, input DriverInput) (*Driver, error) {
	d, ok := m.drivers[driverID]
	if !ok {
		return nil, ErrNotFound
	}
	d.Status = input.Status
	return d, nil
}

func (m *mockRepo) Delete(_ context.Context, driverID int64) error {
	if _, ok := m.drivers[driverID]; !ok {
		return ErrNotFound
	}
	delete(m.drivers, driverID)
	return nil
}

func (m *mockRepo) Upsert(_ context.Context, userID int64, req ApplyDriverRequest) (*Driver, error) {
	if d, ok := m.byUserID[userID]; ok {
		return d, nil
	}
	id := m.next()
	d := &Driver{ID: id, UserID: &userID, Status: "ACTIVE", ApprovalStatus: "PENDING"}
	m.drivers[id] = d
	m.byUserID[userID] = d
	return d, nil
}

func (m *mockRepo) UserOwnsANursery(_ context.Context, userID int64) (bool, error) {
	return m.owners[userID], nil
}

func (m *mockRepo) Approve(_ context.Context, driverUserID int64, approvedByUserID int64) (*Driver, error) {
	d, ok := m.byUserID[driverUserID]
	if !ok {
		return nil, ErrNotFound
	}
	d.ApprovalStatus = "APPROVED"
	d.ApprovedByUserID = &approvedByUserID
	return d, nil
}

func (m *mockRepo) CreateLocation(_ context.Context, driverID, actorID int64, input LocationRequest) (*DriverLocation, error) {
	loc := &DriverLocation{ID: m.next(), DriverID: driverID, Latitude: input.Latitude, Longitude: input.Longitude, CreatedBy: &actorID}
	return loc, nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func superActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"SUPER_ADMIN"}} }
func driverActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }
func buyerActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func ownerActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }

func ptr[T any](v T) *T { return &v }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── List (admin-only) ────────────────────────────────────────────────────────

func TestList_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, nil, "ACTIVE")

	drivers, _, err := svc(repo).List(context.Background(), adminActor(1), ListDriversRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(drivers) != 1 {
		t.Errorf("want 1 driver, got %d", len(drivers))
	}
}

func TestList_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), buyerActor(1), ListDriversRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestList_DriverForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), driverActor(1), ListDriversRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for driver role, got %v", err)
	}
}

// ─── Get ──────────────────────────────────────────────────────────────────────

func TestGet_AdminCanViewAny(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(99)), "ACTIVE")

	_, err := svc(repo).Get(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGet_DriverCanViewOwn(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(10)), "ACTIVE")

	_, err := svc(repo).Get(context.Background(), driverActor(10), 1)
	if err != nil {
		t.Fatalf("driver should view own record: %v", err)
	}
}

func TestGet_DriverCannotViewOther(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(99)), "ACTIVE") // belongs to user 99

	_, err := svc(repo).Get(context.Background(), driverActor(10), 1) // actor is user 10
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for other driver's record, got %v", err)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create (admin-only) ──────────────────────────────────────────────────────

func TestCreate_AdminSuccess(t *testing.T) {
	d, err := svc(newMock()).Create(context.Background(), adminActor(1), DriverRequest{
		Status: "ACTIVE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Status != "ACTIVE" {
		t.Errorf("want ACTIVE, got %s", d.Status)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), DriverRequest{Status: "ACTIVE"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_BadStatusInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), DriverRequest{Status: "RETIRED"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad status, got %v", err)
	}
}

func TestCreate_BadLicenseDateInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), DriverRequest{
		Status:            "ACTIVE",
		LicenseExpiryDate: ptr("not-a-date"),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad date, got %v", err)
	}
}

func TestCreate_ValidLicenseDateAccepted(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), DriverRequest{
		Status:            "ACTIVE",
		LicenseExpiryDate: ptr("2030-12-31"),
	})
	if err != nil {
		t.Fatalf("valid date should be accepted: %v", err)
	}
}

func TestCreate_DuplicateReturnsError(t *testing.T) {
	repo := newMock()
	lic := "MH01AB1234"
	repo.duplicates[lic] = true

	_, err := svc(repo).Create(context.Background(), adminActor(1), DriverRequest{
		LicenseNumber: &lic,
		Status:        "ACTIVE",
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("want ErrDuplicate, got %v", err)
	}
}

// ─── Apply (self-register) ───────────────────────────────────────────────────

func TestApply_Success(t *testing.T) {
	d, err := svc(newMock()).Apply(context.Background(), buyerActor(10), ApplyDriverRequest{
		LicenceNumber: "MH01AB1234",
		VehicleNumber: "MH01CD5678",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.UserID == nil || *d.UserID != 10 {
		t.Errorf("want UserID 10, got %v", d.UserID)
	}
}

func TestApply_EmptyLicenceInvalid(t *testing.T) {
	_, err := svc(newMock()).Apply(context.Background(), buyerActor(10), ApplyDriverRequest{
		LicenceNumber: "   ",
		VehicleNumber: "MH01CD5678",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty licence, got %v", err)
	}
}

func TestApply_EmptyVehicleNumberInvalid(t *testing.T) {
	_, err := svc(newMock()).Apply(context.Background(), buyerActor(10), ApplyDriverRequest{
		LicenceNumber: "MH01AB1234",
		VehicleNumber: "   ",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty vehicle number, got %v", err)
	}
}

func TestApply_NurseryOwnerForbidden(t *testing.T) {
	repo := newMock()
	repo.owners[10] = true // user 10 owns a nursery

	_, err := svc(repo).Apply(context.Background(), ownerActor(10), ApplyDriverRequest{
		LicenceNumber: "MH01AB1234",
		VehicleNumber: "MH01CD5678",
	})
	if !errors.Is(err, ErrOwnerCannotBeDriver) {
		t.Errorf("want ErrOwnerCannotBeDriver, got %v", err)
	}
}

// ─── GetMine ──────────────────────────────────────────────────────────────────

func TestGetMine_Success(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(10)), "ACTIVE")

	d, err := svc(repo).GetMine(context.Background(), driverActor(10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.UserID == nil || *d.UserID != 10 {
		t.Errorf("want UserID 10, got %v", d.UserID)
	}
}

func TestGetMine_NotFound(t *testing.T) {
	_, err := svc(newMock()).GetMine(context.Background(), driverActor(99))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Approve ──────────────────────────────────────────────────────────────────

func TestApprove_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(10)), "ACTIVE")

	d, err := svc(repo).Approve(context.Background(), adminActor(5), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ApprovalStatus != "APPROVED" {
		t.Errorf("want APPROVED, got %s", d.ApprovalStatus)
	}
}

func TestApprove_SuperAdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(10)), "ACTIVE")

	_, err := svc(repo).Approve(context.Background(), superActor(5), 10)
	if err != nil {
		t.Fatalf("super admin should approve: %v", err)
	}
}

func TestApprove_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Approve(context.Background(), buyerActor(1), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestDelete_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, nil, "ACTIVE")

	err := svc(repo).Delete(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.drivers[1]; ok {
		t.Error("driver should have been deleted")
	}
}

func TestDelete_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).Delete(context.Background(), buyerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── CreateLocation ───────────────────────────────────────────────────────────

func TestCreateLocation_OwnDriver(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(10)), "ACTIVE")

	loc, err := svc(repo).CreateLocation(context.Background(), driverActor(10), 1, LocationRequest{
		Latitude: 12.97, Longitude: 77.59,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loc.Latitude != 12.97 {
		t.Errorf("want lat 12.97, got %v", loc.Latitude)
	}
}

func TestCreateLocation_OtherDriverForbidden(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(99)), "ACTIVE")

	_, err := svc(repo).CreateLocation(context.Background(), driverActor(10), 1, LocationRequest{
		Latitude: 12.97, Longitude: 77.59,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for other driver, got %v", err)
	}
}

func TestCreateLocation_InvalidLatitude(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(10)), "ACTIVE")

	_, err := svc(repo).CreateLocation(context.Background(), driverActor(10), 1, LocationRequest{
		Latitude: 95.0, Longitude: 77.59,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for lat=95, got %v", err)
	}
}

func TestCreateLocation_AdminCanUpdateAny(t *testing.T) {
	repo := newMock()
	repo.seedDriver(1, ptr(int64(99)), "ACTIVE")

	_, err := svc(repo).CreateLocation(context.Background(), adminActor(1), 1, LocationRequest{
		Latitude: 12.97, Longitude: 77.59,
	})
	if err != nil {
		t.Fatalf("admin should update any driver's location: %v", err)
	}
}

// ─── isAllowedStatus helper ───────────────────────────────────────────────────

func TestIsAllowedStatus(t *testing.T) {
	for _, s := range []string{"ACTIVE", "INACTIVE", "SUSPENDED", "DELETED"} {
		if !isAllowedStatus(s) {
			t.Errorf("status %q should be allowed", s)
		}
	}
	for _, s := range []string{"RETIRED", "BANNED", ""} {
		if isAllowedStatus(s) {
			t.Errorf("status %q should NOT be allowed", s)
		}
	}
}
