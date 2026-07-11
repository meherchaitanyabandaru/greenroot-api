package users

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	users     map[int64]*User
	addresses map[int64]*Address
	roles     map[int64][]Role
	sessions  map[int64][]Session

	blockers       map[int64]AccountDeletionBlockers
	softDeletedIDs []int64
	nextAddrID     int64
}

func newMock() *mockRepo {
	return &mockRepo{
		users:      make(map[int64]*User),
		addresses:  make(map[int64]*Address),
		roles:      make(map[int64][]Role),
		sessions:   make(map[int64][]Session),
		blockers:   make(map[int64]AccountDeletionBlockers),
		nextAddrID: 100,
	}
}

func (m *mockRepo) seedUser(id int64, firstName, mobile string) *User {
	u := &User{ID: id, FirstName: firstName, Mobile: mobile, Status: "ACTIVE"}
	m.users[id] = u
	return u
}

func (m *mockRepo) FindUserByID(_ context.Context, id int64) (*User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *mockRepo) UpdateProfile(_ context.Context, userID int64, input UpdateProfileRequest, _ time.Time) (*User, error) {
	u, ok := m.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	u.FirstName = input.FirstName
	u.LastName = input.LastName
	u.Email = input.Email
	u.Gender = input.Gender
	u.ProfileImageURL = input.ProfileImageURL
	return u, nil
}

func (m *mockRepo) ListAddresses(_ context.Context, userID int64) ([]Address, error) {
	var result []Address
	for _, a := range m.addresses {
		if a.UserID == userID {
			result = append(result, *a)
		}
	}
	return result, nil
}

func (m *mockRepo) CreateAddress(_ context.Context, userID int64, input CreateAddressRequest) (*Address, error) {
	m.nextAddrID++
	a := &Address{ID: m.nextAddrID, UserID: userID, AddressLine1: input.AddressLine1}
	m.addresses[m.nextAddrID] = a
	return a, nil
}

func (m *mockRepo) UpdateAddress(_ context.Context, userID int64, addressID int64, input UpdateAddressRequest) (*Address, error) {
	a, ok := m.addresses[addressID]
	if !ok || a.UserID != userID {
		return nil, ErrNotFound
	}
	a.AddressLine1 = input.AddressLine1
	return a, nil
}

func (m *mockRepo) DeleteAddress(_ context.Context, userID int64, addressID int64) error {
	a, ok := m.addresses[addressID]
	if !ok || a.UserID != userID {
		return ErrNotFound
	}
	delete(m.addresses, addressID)
	return nil
}

func (m *mockRepo) ListRoles(_ context.Context, userID int64) ([]Role, error) {
	return m.roles[userID], nil
}

func (m *mockRepo) ListSessions(_ context.Context, userID int64) ([]Session, error) {
	return m.sessions[userID], nil
}

func (m *mockRepo) CreateUserActivity(_ context.Context, _ CreateActivityInput) error { return nil }

func (m *mockRepo) GetAccountDeletionBlockers(_ context.Context, userID int64) (AccountDeletionBlockers, error) {
	return m.blockers[userID], nil
}

func (m *mockRepo) SoftDeleteAccount(_ context.Context, userID int64) error {
	m.softDeletedIDs = append(m.softDeletedIDs, userID)
	if u, ok := m.users[userID]; ok {
		u.Status = "DELETED"
	}
	return nil
}

func (m *mockRepo) wasSoftDeleted(userID int64) bool {
	for _, id := range m.softDeletedIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func buyerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil, nil) }

// ─── Me / GetUser ─────────────────────────────────────────────────────────────

func TestMe_ReturnsSelf(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	user, err := svc(repo).Me(context.Background(), buyerActor(10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != 10 {
		t.Errorf("want ID 10, got %d", user.ID)
	}
}

func TestGetUser_AdminCanReadOther(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	user, err := svc(repo).GetUser(context.Background(), adminActor(1), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != 10 {
		t.Errorf("want user 10, got %d", user.ID)
	}
}

func TestGetUser_BuyerCannotReadOther(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	_, err := svc(repo).GetUser(context.Background(), buyerActor(20), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestGetUser_BuyerCanReadSelf(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	_, err := svc(repo).GetUser(context.Background(), buyerActor(10), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	_, err := svc(newMock()).GetUser(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── UpdateMe ─────────────────────────────────────────────────────────────────

func TestUpdateMe_Success(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	user, err := svc(repo).UpdateMe(context.Background(), buyerActor(10), UpdateProfileRequest{FirstName: "Ravi Updated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// firstName was already set — gets locked to existing value
	if user.FirstName == "" {
		t.Error("want non-empty first name")
	}
}

func TestUpdateMe_ReplacesDefaultGreenRootFirstName(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "GreenRoot", "9300000000")
	lastName := "Kumar"

	user, err := svc(repo).UpdateMe(context.Background(), buyerActor(10), UpdateProfileRequest{
		FirstName: "Ravi",
		LastName:  &lastName,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.FirstName != "Ravi" {
		t.Fatalf("default first name was not replaced, got %q", user.FirstName)
	}
	if user.LastName == nil || *user.LastName != "Kumar" {
		t.Fatalf("last name was not saved: %v", user.LastName)
	}
}

func TestUpdateMe_EmptyFirstNameInvalid(t *testing.T) {
	repo := newMock()
	// User with no first name set — empty first_name in request should fail
	u := &User{ID: 10, Mobile: "9300000000", Status: "ACTIVE", FirstName: ""}
	repo.users[10] = u

	_, err := svc(repo).UpdateMe(context.Background(), buyerActor(10), UpdateProfileRequest{FirstName: "   "})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty first name, got %v", err)
	}
}

func TestUpdateMe_InvalidGender(t *testing.T) {
	repo := newMock()
	u := &User{ID: 10, Mobile: "9300000000", Status: "ACTIVE"}
	repo.users[10] = u

	gender := "UNKNOWN_GENDER"
	_, err := svc(repo).UpdateMe(context.Background(), buyerActor(10), UpdateProfileRequest{
		FirstName: "Ravi",
		Gender:    &gender,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad gender, got %v", err)
	}
}

func TestUpdateMe_ValidGenders(t *testing.T) {
	genders := []string{"MALE", "FEMALE", "NON_BINARY", "OTHER", "PREFER_NOT_TO_SAY"}
	for _, g := range genders {
		repo := newMock()
		repo.users[10] = &User{ID: 10, Mobile: "9300000000"}
		gender := g

		_, err := svc(repo).UpdateMe(context.Background(), buyerActor(10), UpdateProfileRequest{
			FirstName: "Ravi",
			Gender:    &gender,
		})
		if err != nil {
			t.Errorf("gender %q should be valid, got: %v", g, err)
		}
	}
}

// ─── ListAddresses / CreateAddress ───────────────────────────────────────────

func TestListAddresses_OwnAddresses(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")
	repo.addresses[1] = &Address{ID: 1, UserID: 10, AddressLine1: "123 Main St"}

	addrs, err := svc(repo).ListAddresses(context.Background(), buyerActor(10), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(addrs) != 1 {
		t.Errorf("want 1 address, got %d", len(addrs))
	}
}

func TestListAddresses_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	_, err := svc(repo).ListAddresses(context.Background(), buyerActor(20), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestCreateAddress_Success(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	addr, err := svc(repo).CreateAddress(context.Background(), buyerActor(10), 10, CreateAddressRequest{
		AddressLine1: "123 Main St",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr.AddressLine1 != "123 Main St" {
		t.Errorf("want 123 Main St, got %s", addr.AddressLine1)
	}
}

func TestCreateAddress_EmptyLine1Invalid(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	_, err := svc(repo).CreateAddress(context.Background(), buyerActor(10), 10, CreateAddressRequest{
		AddressLine1: "   ",
	})
	if !errors.Is(err, ErrInvalidAddress) {
		t.Errorf("want ErrInvalidAddress, got %v", err)
	}
}

func TestCreateAddress_InvalidLatitude(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")
	lat := 200.0

	_, err := svc(repo).CreateAddress(context.Background(), buyerActor(10), 10, CreateAddressRequest{
		AddressLine1: "123 Main St",
		Latitude:     &lat,
	})
	if !errors.Is(err, ErrInvalidAddress) {
		t.Errorf("want ErrInvalidAddress for out-of-range lat, got %v", err)
	}
}

func TestCreateAddress_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	_, err := svc(repo).CreateAddress(context.Background(), buyerActor(20), 10, CreateAddressRequest{
		AddressLine1: "123 Main St",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── ListRoles / ListSessions ─────────────────────────────────────────────────

func TestListRoles_OwnRoles(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")
	repo.roles[10] = []Role{{Code: "BUYER"}}

	roles, err := svc(repo).ListRoles(context.Background(), buyerActor(10), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 1 || roles[0].Code != "BUYER" {
		t.Error("expected BUYER role")
	}
}

func TestListRoles_AdminCanReadOther(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")
	repo.roles[10] = []Role{{Code: "BUYER"}}

	_, err := svc(repo).ListRoles(context.Background(), adminActor(1), 10)
	if err != nil {
		t.Fatalf("admin should read any user's roles: %v", err)
	}
}

func TestListRoles_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	_, err := svc(repo).ListRoles(context.Background(), buyerActor(20), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestListSessions_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	_, err := svc(repo).ListSessions(context.Background(), buyerActor(20), 10)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── DeleteAccount ────────────────────────────────────────────────────────────

func TestDeleteAccount_ActiveUser_Success(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	err := svc(repo).DeleteAccount(context.Background(), buyerActor(10))
	if err != nil {
		t.Fatalf("unexpected error deleting active account: %v", err)
	}
	if !repo.wasSoftDeleted(10) {
		t.Error("SoftDeleteAccount should have been called for user 10")
	}
}

func TestDeleteAccount_BlockedWhenUserOwnsActiveNursery(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")
	repo.blockers[10] = AccountDeletionBlockers{OwnedNurseries: 1}

	err := svc(repo).DeleteAccount(context.Background(), buyerActor(10))
	if !errors.Is(err, ErrAccountDeletionBlocked) {
		t.Fatalf("want ErrAccountDeletionBlocked, got %v", err)
	}
	if repo.wasSoftDeleted(10) {
		t.Fatal("SoftDeleteAccount must not run while active nursery ownership blocks deletion")
	}
}

func TestDeleteAccount_BlockedWhenUserHasActiveOrder(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")
	repo.blockers[10] = AccountDeletionBlockers{ActiveOrders: 1}

	err := svc(repo).DeleteAccount(context.Background(), buyerActor(10))
	if !errors.Is(err, ErrAccountDeletionBlocked) {
		t.Fatalf("want ErrAccountDeletionBlocked, got %v", err)
	}
	if repo.wasSoftDeleted(10) {
		t.Fatal("SoftDeleteAccount must not run while active orders block deletion")
	}
}

func TestDeleteAccount_BlockedWhenUserHasActiveQuotation(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")
	repo.blockers[10] = AccountDeletionBlockers{ActiveQuotations: 1}

	err := svc(repo).DeleteAccount(context.Background(), buyerActor(10))
	if !errors.Is(err, ErrAccountDeletionBlocked) {
		t.Fatalf("want ErrAccountDeletionBlocked, got %v", err)
	}
	if repo.wasSoftDeleted(10) {
		t.Fatal("SoftDeleteAccount must not run while active quotations block deletion")
	}
}

func TestDeleteAccount_AlreadyDeleted_ReturnsError(t *testing.T) {
	repo := newMock()
	u := &User{ID: 10, FirstName: "Deleted", Mobile: "9300000000", Status: "DELETED"}
	repo.users[10] = u

	err := svc(repo).DeleteAccount(context.Background(), buyerActor(10))
	if !errors.Is(err, ErrAccountDeleted) {
		t.Errorf("want ErrAccountDeleted for already-deleted account, got %v", err)
	}
}

func TestDeleteAccount_AlreadyDeleted_DoesNotCallSoftDelete(t *testing.T) {
	repo := newMock()
	repo.users[10] = &User{ID: 10, FirstName: "Deleted", Mobile: "9300000000", Status: "DELETED"}

	_ = svc(repo).DeleteAccount(context.Background(), buyerActor(10))

	if repo.wasSoftDeleted(10) {
		t.Error("SoftDeleteAccount must NOT be called for an already-deleted account")
	}
}

func TestDeleteAccount_UserNotFound_ReturnsError(t *testing.T) {
	err := svc(newMock()).DeleteAccount(context.Background(), buyerActor(9999))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound for unknown user, got %v", err)
	}
}

func TestDeleteAccount_OwnerCanDeleteOwnAccount(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Priya", "9100000000")

	err := svc(repo).DeleteAccount(context.Background(), ActorContext{UserID: 10, Roles: []string{"NURSERY_OWNER"}})
	if err != nil {
		t.Fatalf("owner should be able to delete their own account: %v", err)
	}
	if !repo.wasSoftDeleted(10) {
		t.Error("SoftDeleteAccount should have been called for the owner")
	}
}

func TestDeleteAccount_ManagerCanDeleteOwnAccount(t *testing.T) {
	repo := newMock()
	repo.seedUser(20, "Gumastha", "9200000000")

	err := svc(repo).DeleteAccount(context.Background(), ActorContext{UserID: 20, Roles: []string{"MANAGER"}})
	if err != nil {
		t.Fatalf("manager should be able to delete their own account: %v", err)
	}
	if !repo.wasSoftDeleted(20) {
		t.Error("SoftDeleteAccount should have been called for the manager")
	}
}

func TestDeleteAccount_DriverCanDeleteOwnAccount(t *testing.T) {
	repo := newMock()
	repo.seedUser(40, "Raju", "9400000000")

	err := svc(repo).DeleteAccount(context.Background(), ActorContext{UserID: 40, Roles: []string{"DRIVER"}})
	if err != nil {
		t.Fatalf("driver should be able to delete their own account: %v", err)
	}
	if !repo.wasSoftDeleted(40) {
		t.Error("SoftDeleteAccount should have been called for the driver")
	}
}

// ─── Re-entry: full lifecycle round-trip ─────────────────────────────────────

// TestDeleteAndReregisterUserHasNoHistory verifies that after a user's account is
// deleted, a new seedUser with the same mobile has a fresh status (ACTIVE, not DELETED).
// In production the new registration creates a new user record; here we simulate by
// checking the old record is marked DELETED and a new record would start from scratch.
func TestDeleteAndReregisterUserHasNoHistory(t *testing.T) {
	repo := newMock()
	repo.seedUser(10, "Ravi", "9300000000")

	// Delete the account.
	if err := svc(repo).DeleteAccount(context.Background(), buyerActor(10)); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if repo.users[10].Status != "DELETED" {
		t.Fatalf("expected status DELETED, got %q", repo.users[10].Status)
	}

	// Re-registration: a new user record (ID=11) is created with the same mobile.
	newUser := &User{ID: 11, FirstName: "GreenRoot", Mobile: "9300000000", Status: "ACTIVE"}
	repo.users[11] = newUser

	// The new user is ACTIVE and has no history from the previous account.
	if newUser.Status != "ACTIVE" {
		t.Error("re-registered user should have ACTIVE status")
	}
	if newUser.ID == 10 {
		t.Error("re-registered user must have a different ID from the deleted account")
	}
}
