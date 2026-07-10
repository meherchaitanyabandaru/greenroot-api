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

	nextAddrID int64
}

func newMock() *mockRepo {
	return &mockRepo{
		users:      make(map[int64]*User),
		addresses:  make(map[int64]*Address),
		roles:      make(map[int64][]Role),
		sessions:   make(map[int64][]Session),
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
