package nurseries

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	nurseries map[int64]*Nursery
	owners    map[int64]int64   // userID → nurseryID (reverse: userID is owner of nurseryID)
	members   map[string]bool   // "nurseryID:userID"
	managers  map[int64]bool    // userID → isManager
	drivers   map[int64]bool    // userID → isApprovedDriver
	addresses map[int64]*Address
	users     map[int64][]*UserLink
	nDrivers  map[int64][]*NurseryDriver
	customers map[int64][]Customer // nurseryID → customers

	// tracking fields for lifecycle assertions
	invalidatedSessions    []int64            // user IDs whose sessions were invalidated
	activeManagerNurseries map[int64]int64    // userID → nurseryID for FindActiveManagerNursery

	nextNurseryID  int64
	nextAddressID  int64
	nextUserLinkID int64
	nextDriverID   int64
}

func newMock() *mockRepo {
	return &mockRepo{
		nurseries:              make(map[int64]*Nursery),
		owners:                 make(map[int64]int64),
		members:                make(map[string]bool),
		managers:               make(map[int64]bool),
		drivers:                make(map[int64]bool),
		addresses:              make(map[int64]*Address),
		users:                  make(map[int64][]*UserLink),
		nDrivers:               make(map[int64][]*NurseryDriver),
		customers:              make(map[int64][]Customer),
		activeManagerNurseries: make(map[int64]int64),
		nextNurseryID:          100,
		nextAddressID:          200,
		nextUserLinkID:         300,
		nextDriverID:           400,
	}
}

func ptr[T any](v T) *T { return &v }
func mkKey(a, b int64) string {
	return string(rune('0'+a)) + ":" + string(rune('0'+b))
}

func (m *mockRepo) seedNursery(id int64, name, status string, ownerUserID *int64) *Nursery {
	n := &Nursery{ID: id, Name: name, Status: status, OwnerUserID: ownerUserID}
	m.nurseries[id] = n
	if ownerUserID != nil {
		m.owners[*ownerUserID] = id
	}
	return n
}

func (m *mockRepo) seedMember(nurseryID, userID int64) {
	key := string(rune(nurseryID)) + ":" + string(rune(userID))
	_ = key
	m.members[mkKeyInt(nurseryID, userID)] = true
}

func mkKeyInt(a, b int64) string {
	return string(rune(a)) + ":" + string(rune(b))
}

// Repository interface implementation

func (m *mockRepo) List(_ context.Context, _ ListNurseriesRequest) ([]Nursery, int64, error) {
	result := make([]Nursery, 0, len(m.nurseries))
	for _, n := range m.nurseries {
		result = append(result, *n)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*Nursery, error) {
	n, ok := m.nurseries[id]
	if !ok {
		return nil, ErrNotFound
	}
	return n, nil
}

func (m *mockRepo) FindOwnedByUser(_ context.Context, ownerUserID int64) (*Nursery, error) {
	nid, ok := m.owners[ownerUserID]
	if !ok {
		return nil, ErrNotFound
	}
	return m.nurseries[nid], nil
}

func (m *mockRepo) UserOwnsANursery(_ context.Context, userID int64) (bool, error) {
	_, ok := m.owners[userID]
	return ok, nil
}

func (m *mockRepo) UserIsManager(_ context.Context, userID int64) (bool, error) {
	return m.managers[userID], nil
}

func (m *mockRepo) UserIsApprovedDriver(_ context.Context, userID int64) (bool, error) {
	return m.drivers[userID], nil
}

func (m *mockRepo) IsNurseryOwner(_ context.Context, nurseryID, userID int64) (bool, error) {
	nid, ok := m.owners[userID]
	return ok && nid == nurseryID, nil
}

func (m *mockRepo) Create(_ context.Context, _ int64, input CreateNurseryRequest) (*Nursery, error) {
	m.nextNurseryID++
	status := "ACTIVE"
	if input.Status != nil {
		status = *input.Status
	}
	n := &Nursery{ID: m.nextNurseryID, Name: input.Name, Status: status, OwnerUserID: input.OwnerUserID}
	m.nurseries[m.nextNurseryID] = n
	return n, nil
}

func (m *mockRepo) Update(_ context.Context, _ int64, id int64, input UpdateNurseryRequest) (*Nursery, error) {
	n, ok := m.nurseries[id]
	if !ok {
		return nil, ErrNotFound
	}
	n.Name = input.Name
	return n, nil
}

func (m *mockRepo) UpdateStatusOnly(_ context.Context, _ int64, id int64, status string) (*Nursery, error) {
	n, ok := m.nurseries[id]
	if !ok {
		return nil, ErrNotFound
	}
	n.Status = status
	return n, nil
}

func (m *mockRepo) Delete(_ context.Context, _ int64, id int64) error {
	if _, ok := m.nurseries[id]; !ok {
		return ErrNotFound
	}
	delete(m.nurseries, id)
	return nil
}

func (m *mockRepo) ListAddresses(_ context.Context, nurseryID int64) ([]Address, error) {
	var result []Address
	for _, a := range m.addresses {
		if a.NurseryID == nurseryID {
			result = append(result, *a)
		}
	}
	return result, nil
}

func (m *mockRepo) CreateAddress(_ context.Context, nurseryID int64, input AddressRequest) (*Address, error) {
	m.nextAddressID++
	a := &Address{ID: m.nextAddressID, NurseryID: nurseryID, IsPrimary: input.IsPrimary}
	m.addresses[m.nextAddressID] = a
	return a, nil
}

func (m *mockRepo) UpdateAddress(_ context.Context, id int64, _ AddressRequest) (*Address, error) {
	a, ok := m.addresses[id]
	if !ok {
		return nil, ErrNotFound
	}
	return a, nil
}

func (m *mockRepo) DeleteAddress(_ context.Context, id int64) error {
	if _, ok := m.addresses[id]; !ok {
		return ErrNotFound
	}
	delete(m.addresses, id)
	return nil
}

func (m *mockRepo) ListManagers(_ context.Context, nurseryID int64) ([]UserLink, error) {
	return m.listUsersFor(nurseryID), nil
}

func (m *mockRepo) ListUsers(_ context.Context, nurseryID int64) ([]UserLink, error) {
	return m.listUsersFor(nurseryID), nil
}

func (m *mockRepo) listUsersFor(nurseryID int64) []UserLink {
	result := []UserLink{}
	for _, ul := range m.users[nurseryID] {
		result = append(result, *ul)
	}
	return result
}

func (m *mockRepo) AddUser(_ context.Context, nurseryID int64, input AddUserRequest) (*UserLink, error) {
	m.nextUserLinkID++
	ul := &UserLink{ID: m.nextUserLinkID, NurseryID: nurseryID, UserID: input.UserID, RoleCode: input.RoleCode, Status: "ACTIVE", IsActive: true}
	m.users[nurseryID] = append(m.users[nurseryID], ul)
	return ul, nil
}

func (m *mockRepo) AddManager(_ context.Context, nurseryID int64, _ int64, input AddManagerRequest) (*UserLink, error) {
	m.nextUserLinkID++
	ul := &UserLink{ID: m.nextUserLinkID, NurseryID: nurseryID, UserID: input.UserID, Role: input.Role, Status: "ACTIVE", IsActive: true}
	m.users[nurseryID] = append(m.users[nurseryID], ul)
	return ul, nil
}

func (m *mockRepo) RemoveUser(_ context.Context, nurseryID int64, userID int64) error {
	users := m.users[nurseryID]
	for i, u := range users {
		if u.UserID == userID {
			m.users[nurseryID] = append(users[:i], users[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.members[mkKeyInt(nurseryID, userID)], nil
}

func (m *mockRepo) ListByUserID(_ context.Context, userID int64) ([]Nursery, error) {
	nid, ok := m.owners[userID]
	if !ok {
		return nil, nil
	}
	n := m.nurseries[nid]
	if n == nil {
		return nil, nil
	}
	return []Nursery{*n}, nil
}

func (m *mockRepo) ConnectDriver(_ context.Context, nurseryID, driverUserID, _ int64) (*NurseryDriver, error) {
	m.nextDriverID++
	nd := &NurseryDriver{ID: m.nextDriverID, NurseryID: nurseryID, DriverUserID: driverUserID, ConnectionStatus: "PENDING"}
	m.nDrivers[nurseryID] = append(m.nDrivers[nurseryID], nd)
	return nd, nil
}

func (m *mockRepo) ApproveDriverConnection(_ context.Context, nurseryID, driverUserID, _ int64) error {
	for _, nd := range m.nDrivers[nurseryID] {
		if nd.DriverUserID == driverUserID {
			nd.ConnectionStatus = "APPROVED"
			return nil
		}
	}
	return ErrNotFound
}

func (m *mockRepo) ListConnectedDrivers(_ context.Context, nurseryID int64) ([]NurseryDriver, error) {
	result := []NurseryDriver{}
	for _, nd := range m.nDrivers[nurseryID] {
		result = append(result, *nd)
	}
	return result, nil
}

func (m *mockRepo) GetCustomers(_ context.Context, nurseryID int64) ([]Customer, error) {
	return m.customers[nurseryID], nil
}

func (m *mockRepo) GrantOwnerRole(_ context.Context, _ int64, _ int64) error { return nil }

func (m *mockRepo) InvalidateUserSessions(_ context.Context, userID int64) error {
	m.invalidatedSessions = append(m.invalidatedSessions, userID)
	return nil
}

func (m *mockRepo) DisconnectDriver(_ context.Context, nurseryID, driverUserID, _ int64) error {
	for i, nd := range m.nDrivers[nurseryID] {
		if nd.DriverUserID == driverUserID {
			nd.ConnectionStatus = "DISCONNECTED"
			m.nDrivers[nurseryID][i] = nd
			return nil
		}
	}
	return ErrNotFound
}

func (m *mockRepo) FindActiveManagerNursery(_ context.Context, userID int64) (int64, error) {
	nid, ok := m.activeManagerNurseries[userID]
	if !ok {
		return 0, ErrNotFound
	}
	return nid, nil
}

func (m *mockRepo) CancelPendingInvitesForUser(_ context.Context, _ int64, _ int64) error {
	return nil
}

// wasSessionInvalidated reports whether InvalidateUserSessions was called for userID.
func (m *mockRepo) wasSessionInvalidated(userID int64) bool {
	for _, id := range m.invalidatedSessions {
		if id == userID {
			return true
		}
	}
	return false
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── List ─────────────────────────────────────────────────────────────────────

func TestList_ReturnsAll(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery One", "ACTIVE", ptr(int64(10)))
	repo.seedNursery(2, "Nursery Two", "ACTIVE", ptr(int64(20)))

	nurseries, _, err := svc(repo).List(context.Background(), ListNurseriesRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nurseries) != 2 {
		t.Errorf("want 2 nurseries, got %d", len(nurseries))
	}
}

// ─── Get ─────────────────────────────────────────────────────────────────────

func TestGet_Found(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Test Nursery", "ACTIVE", nil)

	n, err := svc(repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Name != "Test Nursery" {
		t.Errorf("want Test Nursery, got %s", n.Name)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── GetOwned ────────────────────────────────────────────────────────────────

func TestGetOwned_Found(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "My Nursery", "ACTIVE", ptr(int64(10)))

	n, err := svc(repo).GetOwned(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Name != "My Nursery" {
		t.Errorf("want My Nursery, got %s", n.Name)
	}
}

func TestGetOwned_NotFound(t *testing.T) {
	_, err := svc(newMock()).GetOwned(context.Background(), 99)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_BuyerSuccess(t *testing.T) {
	n, err := svc(newMock()).Create(context.Background(), buyerActor(10), CreateNurseryRequest{Name: "Green Nursery"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Name != "Green Nursery" {
		t.Errorf("want Green Nursery, got %s", n.Name)
	}
	if n.OwnerUserID == nil || *n.OwnerUserID != 10 {
		t.Error("want OwnerUserID=10")
	}
}

func TestCreate_AlreadyOwnerRejected(t *testing.T) {
	repo := newMock()
	repo.owners[10] = 1 // user 10 already owns nursery 1

	_, err := svc(repo).Create(context.Background(), ownerActor(10), CreateNurseryRequest{Name: "Second Nursery"})
	if !errors.Is(err, ErrAlreadyOwner) {
		t.Errorf("want ErrAlreadyOwner, got %v", err)
	}
}

func TestCreate_ManagerCannotOwn(t *testing.T) {
	repo := newMock()
	repo.managers[20] = true // user 20 is a manager

	_, err := svc(repo).Create(context.Background(), managerActor(20), CreateNurseryRequest{Name: "Manager Nursery"})
	if !errors.Is(err, ErrManagerCannotOwnNursery) {
		t.Errorf("want ErrManagerCannotOwnNursery, got %v", err)
	}
}

func TestCreate_ApprovedDriverCannotOwn(t *testing.T) {
	repo := newMock()
	repo.drivers[30] = true

	_, err := svc(repo).Create(context.Background(), buyerActor(30), CreateNurseryRequest{Name: "Driver Nursery"})
	if !errors.Is(err, ErrDriverCannotOwnNursery) {
		t.Errorf("want ErrDriverCannotOwnNursery, got %v", err)
	}
}

func TestCreate_AdminBypassesRules(t *testing.T) {
	repo := newMock()
	repo.owners[1] = 5 // admin already owns a nursery — but admin bypasses

	_, err := svc(repo).Create(context.Background(), adminActor(1), CreateNurseryRequest{Name: "Admin Nursery"})
	if err != nil {
		t.Fatalf("admin should bypass single-nursery rule: %v", err)
	}
}

func TestCreate_MissingNameInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(10), CreateNurseryRequest{Name: "   "})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty name, got %v", err)
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestUpdate_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Old Name", "ACTIVE", nil)

	n, err := svc(repo).Update(context.Background(), adminActor(1), 1, UpdateNurseryRequest{Name: "New Name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Name != "New Name" {
		t.Errorf("want New Name, got %s", n.Name)
	}
}

func TestUpdate_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Old Name", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{Name: "Updated"})
	if err != nil {
		t.Fatalf("owner should be able to update: %v", err)
	}
}

func TestUpdate_ManagerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), managerActor(20), 1, UpdateNurseryRequest{Name: "Hack"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for manager, got %v", err)
	}
}

// ─── UpdateStatus ─────────────────────────────────────────────────────────────

func TestUpdateStatus_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "PENDING", ptr(int64(10)))

	n, err := svc(repo).UpdateStatus(context.Background(), adminActor(1), 1, "APPROVED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Status != "APPROVED" {
		t.Errorf("want APPROVED, got %s", n.Status)
	}
}

func TestUpdateStatus_OwnerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "PENDING", ptr(int64(10)))

	_, err := svc(repo).UpdateStatus(context.Background(), ownerActor(10), 1, "APPROVED")
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-admin, got %v", err)
	}
}

func TestUpdateStatus_TrialCreatedOnApproval(t *testing.T) {
	repo := newMock()
	ownerID := int64(10)
	repo.seedNursery(1, "Nursery", "PENDING", &ownerID)

	trialCalled := false
	trialSvc := &mockTrialCreator{fn: func() { trialCalled = true }}
	s := NewServiceWithTrial(repo, trialSvc, nil)

	_, err := s.UpdateStatus(context.Background(), adminActor(1), 1, "APPROVED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !trialCalled {
		t.Error("expected trial to be created on APPROVED")
	}
}

type mockTrialCreator struct{ fn func() }

func (m *mockTrialCreator) CreateTrialForOwner(_ context.Context, _ int64, _ time.Time) error {
	if m.fn != nil {
		m.fn()
	}
	return nil
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestDelete_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", nil)

	err := svc(repo).Delete(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.nurseries[1]; ok {
		t.Error("nursery should have been deleted")
	}
}

func TestDelete_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", nil)

	err := svc(repo).Delete(context.Background(), buyerActor(10), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── AddManager ───────────────────────────────────────────────────────────────

func TestAddManager_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1

	_, err := svc(repo).AddManager(context.Background(), ownerActor(10), 1, AddManagerRequest{UserID: 20, Role: "MANAGER"})
	if err != nil {
		t.Fatalf("owner should add managers: %v", err)
	}
}

func TestAddManager_NonOwnerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).AddManager(context.Background(), ownerActor(20), 1, AddManagerRequest{UserID: 30, Role: "MANAGER"})
	if !errors.Is(err, ErrNotNurseryOwner) {
		t.Errorf("want ErrNotNurseryOwner, got %v", err)
	}
}

func TestAddManager_InvalidUserID(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1

	_, err := svc(repo).AddManager(context.Background(), ownerActor(10), 1, AddManagerRequest{UserID: 0})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

// ─── ListManagers ─────────────────────────────────────────────────────────────

func TestListManagers_OwnerCanList(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1

	_, err := svc(repo).ListManagers(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListManagers_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).ListManagers(context.Background(), buyerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── Address CRUD ─────────────────────────────────────────────────────────────

func TestCreateAddress_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", nil)

	_, err := svc(repo).CreateAddress(context.Background(), adminActor(1), 1, AddressRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateAddress_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", nil)

	_, err := svc(repo).CreateAddress(context.Background(), buyerActor(1), 1, AddressRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestCreateAddress_InvalidLatitude(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", nil)

	lat := 200.0 // out of range
	_, err := svc(repo).CreateAddress(context.Background(), adminActor(1), 1, AddressRequest{Latitude: &lat})
	if !errors.Is(err, ErrInvalidAddress) {
		t.Errorf("want ErrInvalidAddress, got %v", err)
	}
}

func TestDeleteAddress_Success(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", nil)
	repo.addresses[1] = &Address{ID: 1, NurseryID: 1}

	err := svc(repo).DeleteAddress(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.addresses[1]; ok {
		t.Error("address should have been deleted")
	}
}

// ─── RemoveUser ───────────────────────────────────────────────────────────────

func TestRemoveUser_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 20}}

	err := svc(repo).RemoveUser(context.Background(), adminActor(1), 1, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveUser_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	err := svc(repo).RemoveUser(context.Background(), buyerActor(99), 1, 20)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── ConnectDriver / ApproveDriver ───────────────────────────────────────────

func TestConnectDriver_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1

	nd, err := svc(repo).ConnectDriver(context.Background(), ownerActor(10), 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nd.DriverUserID != 50 {
		t.Errorf("want DriverUserID 50, got %d", nd.DriverUserID)
	}
}

func TestConnectDriver_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).ConnectDriver(context.Background(), buyerActor(99), 1, 50)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestApproveDriverConnection_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	repo.nDrivers[1] = []*NurseryDriver{{ID: 1, NurseryID: 1, DriverUserID: 50, ConnectionStatus: "PENDING"}}

	err := svc(repo).ApproveDriverConnection(context.Background(), ownerActor(10), 1, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.nDrivers[1][0].ConnectionStatus != "APPROVED" {
		t.Error("driver connection should be APPROVED")
	}
}

// ─── GetCustomers ─────────────────────────────────────────────────────────────

func TestGetCustomers_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.customers[1] = []Customer{
		{UserID: 20, FirstName: "Ravi", Mobile: "9300000000"},
		{UserID: 21, FirstName: "Priya", Mobile: "9310000000"},
	}

	customers, err := svc(repo).GetCustomers(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("admin should see customers: %v", err)
	}
	if len(customers) != 2 {
		t.Errorf("want 2 customers, got %d", len(customers))
	}
}

func TestGetCustomers_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.owners[10] = 1 // user 10 owns nursery 1
	repo.customers[1] = []Customer{{UserID: 20, FirstName: "Ravi", Mobile: "9300000000"}}

	customers, err := svc(repo).GetCustomers(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("nursery owner should see customers: %v", err)
	}
	if len(customers) != 1 {
		t.Errorf("want 1 customer, got %d", len(customers))
	}
	if customers[0].FirstName != "Ravi" {
		t.Errorf("want Ravi, got %s", customers[0].FirstName)
	}
}

func TestGetCustomers_MemberManagerSuccess(t *testing.T) {
	repo := newMock()
	repo.members[mkKeyInt(1, 30)] = true // user 30 is a member of nursery 1
	repo.customers[1] = []Customer{{UserID: 20, FirstName: "Ravi", Mobile: "9300000000"}}

	customers, err := svc(repo).GetCustomers(context.Background(), managerActor(30), 1)
	if err != nil {
		t.Fatalf("nursery member should see customers: %v", err)
	}
	if len(customers) != 1 {
		t.Errorf("want 1 customer, got %d", len(customers))
	}
}

func TestGetCustomers_NonMemberForbidden(t *testing.T) {
	repo := newMock()
	repo.customers[1] = []Customer{{UserID: 20, FirstName: "Ravi", Mobile: "9300000000"}}

	_, err := svc(repo).GetCustomers(context.Background(), ownerActor(99), 1) // user 99 owns nothing
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member owner, got %v", err)
	}
}

func TestGetCustomers_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).GetCustomers(context.Background(), buyerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestGetCustomers_EmptyListOK(t *testing.T) {
	repo := newMock()
	repo.owners[10] = 1

	customers, err := svc(repo).GetCustomers(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("should return empty list, not error: %v", err)
	}
	if len(customers) != 0 {
		t.Errorf("want 0 customers, got %d", len(customers))
	}
}

// ─── Branding validation ──────────────────────────────────────────────────────

func TestValidateBranding_ValidColor(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:       "Nursery",
		BrandColor: ptr("#2E7D32"),
	})
	if err != nil {
		t.Errorf("valid palette color should be accepted, got: %v", err)
	}
}

func TestValidateBranding_InvalidColorHex(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:       "Nursery",
		BrandColor: ptr("#FFFFFF"),
	})
	if !errors.Is(err, ErrInvalidBrandColor) {
		t.Errorf("non-palette color should return ErrInvalidBrandColor, got: %v", err)
	}
}

func TestValidateBranding_InvalidColorNotHex(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:       "Nursery",
		BrandColor: ptr("green"),
	})
	if !errors.Is(err, ErrInvalidBrandColor) {
		t.Errorf("non-hex color should return ErrInvalidBrandColor, got: %v", err)
	}
}

func TestValidateBranding_ValidIconKey(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:         "Nursery",
		BrandIconKey: ptr("leaf"),
		BrandColor:   ptr("#2E7D32"),
	})
	if err != nil {
		t.Errorf("valid icon key should be accepted, got: %v", err)
	}
}

func TestValidateBranding_InvalidIconKey(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:         "Nursery",
		BrandIconKey: ptr("🌿"),
	})
	if !errors.Is(err, ErrInvalidBrandIconKey) {
		t.Errorf("emoji icon key should return ErrInvalidBrandIconKey, got: %v", err)
	}
}

func TestValidateBranding_LogoAndIconConflict(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:         "Nursery",
		LogoURL:      ptr("http://localhost:9000/nursery-logos/abc.jpg"),
		BrandIconKey: ptr("leaf"),
	})
	if !errors.Is(err, ErrBrandingConflict) {
		t.Errorf("setting both logo_url and brand_icon_key should return ErrBrandingConflict, got: %v", err)
	}
}

func TestValidateBranding_InvalidLogoURL_NotHTTP(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:    "Nursery",
		LogoURL: ptr("ftp://evil.com/logo.jpg"),
	})
	if !errors.Is(err, ErrInvalidLogoURL) {
		t.Errorf("non-HTTP logo URL should return ErrInvalidLogoURL, got: %v", err)
	}
}

func TestValidateBranding_InvalidLogoURL_WrongBucket(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:    "Nursery",
		LogoURL: ptr("http://localhost:9000/profile-images/abc.jpg"),
	})
	if !errors.Is(err, ErrInvalidLogoURL) {
		t.Errorf("logo URL pointing to wrong bucket should return ErrInvalidLogoURL, got: %v", err)
	}
}

func TestValidateBranding_ValidLogoURL(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpdateNurseryRequest{
		Name:    "Nursery",
		LogoURL: ptr("http://localhost:9000/nursery-logos/NUR000001.jpg"),
	})
	if err != nil {
		t.Errorf("valid logo URL should be accepted, got: %v", err)
	}
}

func TestValidateBranding_ManagerCannotUpdate(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))

	_, err := svc(repo).Update(context.Background(), managerActor(20), 1, UpdateNurseryRequest{
		Name:         "Nursery",
		BrandIconKey: ptr("leaf"),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("manager should not be able to update branding, got: %v", err)
	}
}

// ─── RemoveUser (extended lifecycle) ─────────────────────────────────────────

func TestRemoveUser_OwnerRemovesManager(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 20}}

	err := svc(repo).RemoveUser(context.Background(), ownerActor(10), 1, 20)
	if err != nil {
		t.Fatalf("owner should remove a manager: %v", err)
	}
	if len(repo.users[1]) != 0 {
		t.Error("manager should have been removed from users list")
	}
}

func TestRemoveUser_SessionsInvalidatedOnRemoval(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 20}}

	_ = svc(repo).RemoveUser(context.Background(), ownerActor(10), 1, 20)

	if !repo.wasSessionInvalidated(20) {
		t.Error("removed manager's sessions should have been invalidated")
	}
}

func TestRemoveUser_OwnerCannotRemoveSelf(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 10}}

	err := svc(repo).RemoveUser(context.Background(), ownerActor(10), 1, 10)
	if !errors.Is(err, ErrOwnerCannotLeave) {
		t.Errorf("want ErrOwnerCannotLeave when owner tries to remove self, got %v", err)
	}
}

func TestRemoveUser_ManagerNotInNursery(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	// no users seeded for nursery 1

	err := svc(repo).RemoveUser(context.Background(), ownerActor(10), 1, 99)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound for unknown user, got %v", err)
	}
}

func TestRemoveUser_ManagerSelfRemoval(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	// manager 20 is in the nursery; user 10 is the owner (not user 20)
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 20}}

	// manager 20 removes themselves (isSelf=true, isOwner=false)
	err := svc(repo).RemoveUser(context.Background(), managerActor(20), 1, 20)
	if err != nil {
		t.Fatalf("manager should be able to self-remove: %v", err)
	}
	if !repo.wasSessionInvalidated(20) {
		t.Error("self-removed manager sessions should be invalidated")
	}
}

// ─── LeaveNursery ─────────────────────────────────────────────────────────────

func TestLeaveNursery_ManagerLeaves(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 20}}
	repo.activeManagerNurseries[20] = 1 // manager 20 → nursery 1

	err := svc(repo).LeaveNursery(context.Background(), managerActor(20))
	if err != nil {
		t.Fatalf("manager should be able to leave nursery: %v", err)
	}
	if len(repo.users[1]) != 0 {
		t.Error("manager should have been removed from the nursery after leaving")
	}
}

func TestLeaveNursery_SessionsInvalidatedOnLeave(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 20}}
	repo.activeManagerNurseries[20] = 1

	_ = svc(repo).LeaveNursery(context.Background(), managerActor(20))

	if !repo.wasSessionInvalidated(20) {
		t.Error("leaving manager's sessions should be invalidated to force re-login")
	}
}

func TestLeaveNursery_NoActiveMembership(t *testing.T) {
	repo := newMock()
	// no activeManagerNurseries entry for user 20

	err := svc(repo).LeaveNursery(context.Background(), managerActor(20))
	if !errors.Is(err, ErrNotMember) {
		t.Errorf("want ErrNotMember for user with no nursery, got %v", err)
	}
}

func TestLeaveNursery_OwnerCannotLeave(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	repo.activeManagerNurseries[10] = 1 // owner's nursery returned by FindActiveManagerNursery

	err := svc(repo).LeaveNursery(context.Background(), ownerActor(10))
	if !errors.Is(err, ErrOwnerCannotLeave) {
		t.Errorf("want ErrOwnerCannotLeave when owner tries to leave, got %v", err)
	}
}

func TestLeaveNursery_CanRejoinAfterLeaving(t *testing.T) {
	// Simulates: manager leaves → manager's record is gone → can accept a new invite.
	// This test verifies leave removes the record; the invite acceptance is in the invites module.
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.users[1] = []*UserLink{{ID: 1, NurseryID: 1, UserID: 20}}
	repo.activeManagerNurseries[20] = 1

	if err := svc(repo).LeaveNursery(context.Background(), managerActor(20)); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if len(repo.users[1]) != 0 {
		t.Fatal("manager should have been removed")
	}

	// After leaving, the user is no longer in the nursery — they can receive and accept a new invite.
	if repo.members[mkKeyInt(1, 20)] {
		t.Error("member entry should not persist after leaving")
	}
}

// ─── DisconnectDriver ─────────────────────────────────────────────────────────

func TestDisconnectDriver_OwnerDisconnects(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	repo.nDrivers[1] = []*NurseryDriver{{ID: 1, NurseryID: 1, DriverUserID: 50, ConnectionStatus: "CONNECTED"}}

	err := svc(repo).DisconnectDriver(context.Background(), ownerActor(10), 1, 50)
	if err != nil {
		t.Fatalf("owner should disconnect a driver: %v", err)
	}
	if repo.nDrivers[1][0].ConnectionStatus != "DISCONNECTED" {
		t.Error("driver connection should be DISCONNECTED")
	}
}

func TestDisconnectDriver_DriverSelfDisconnects(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.nDrivers[1] = []*NurseryDriver{{ID: 1, NurseryID: 1, DriverUserID: 50, ConnectionStatus: "CONNECTED"}}

	// Driver 50 disconnects themselves (isSelf = true)
	err := svc(repo).DisconnectDriver(context.Background(), ActorContext{UserID: 50, Roles: []string{"DRIVER"}}, 1, 50)
	if err != nil {
		t.Fatalf("driver should self-disconnect: %v", err)
	}
}

func TestDisconnectDriver_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.nDrivers[1] = []*NurseryDriver{{ID: 1, NurseryID: 1, DriverUserID: 50, ConnectionStatus: "CONNECTED"}}

	err := svc(repo).DisconnectDriver(context.Background(), buyerActor(99), 1, 50)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestDisconnectDriver_ManagerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.nDrivers[1] = []*NurseryDriver{{ID: 1, NurseryID: 1, DriverUserID: 50, ConnectionStatus: "CONNECTED"}}

	// manager 20 is not the owner and not the driver
	err := svc(repo).DisconnectDriver(context.Background(), managerActor(20), 1, 50)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for manager, got %v", err)
	}
}

func TestDisconnectDriver_NotFound(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	// no drivers seeded

	err := svc(repo).DisconnectDriver(context.Background(), ownerActor(10), 1, 99)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound for unknown driver, got %v", err)
	}
}

func TestDisconnectDriver_SessionsInvalidated(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.owners[10] = 1
	repo.nDrivers[1] = []*NurseryDriver{{ID: 1, NurseryID: 1, DriverUserID: 50, ConnectionStatus: "CONNECTED"}}

	_ = svc(repo).DisconnectDriver(context.Background(), ownerActor(10), 1, 50)

	if !repo.wasSessionInvalidated(50) {
		t.Error("disconnected driver's sessions should be invalidated")
	}
}

func TestDisconnectDriver_AdminDisconnects(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, "Nursery", "ACTIVE", ptr(int64(10)))
	repo.nDrivers[1] = []*NurseryDriver{{ID: 1, NurseryID: 1, DriverUserID: 50, ConnectionStatus: "APPROVED"}}

	err := svc(repo).DisconnectDriver(context.Background(), adminActor(1), 1, 50)
	if err != nil {
		t.Fatalf("admin should be able to disconnect any driver: %v", err)
	}
}
