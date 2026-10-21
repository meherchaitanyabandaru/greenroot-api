package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	summary      Summary
	users        []User
	userStatuses map[int64]string
	nursStatuses map[int64]string
	ownerID      int64
}

func newMock() *mockRepo {
	return &mockRepo{
		userStatuses: make(map[int64]string),
		nursStatuses: make(map[int64]string),
	}
}

func (m *mockRepo) Summary(_ context.Context) (Summary, error) {
	return m.summary, nil
}

func (m *mockRepo) ListUsers(_ context.Context, _ ListUsersRequest) ([]User, int64, error) {
	return m.users, int64(len(m.users)), nil
}

func (m *mockRepo) UpdateUserStatus(_ context.Context, _ int64, userID int64, status string, _ string) error {
	m.userStatuses[userID] = status
	return nil
}

func (m *mockRepo) UpdateNurseryStatus(_ context.Context, _ int64, nurseryID int64, status string, _ string) error {
	m.nursStatuses[nurseryID] = status
	return nil
}

func (m *mockRepo) WorkspaceUserIDs(_ context.Context, nurseryID int64) ([]int64, error) {
	return []int64{nurseryID + 100}, nil
}

func (m *mockRepo) NurseryOwnerID(_ context.Context, _ int64) (int64, error) {
	return m.ownerID, nil
}

func (m *mockRepo) OwnsSuspendedNursery(_ context.Context, userID int64) (bool, error) {
	return userID == m.ownerID && m.nursStatuses[5] == "SUSPENDED", nil
}

func (m *mockRepo) UserStatus(_ context.Context, userID int64) (string, error) {
	if st, ok := m.userStatuses[userID]; ok {
		return st, nil
	}
	return "ACTIVE", nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func superActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"SUPER_ADMIN"}}
}
func buyerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}

func svc(repo *mockRepo) *Service { return NewService(repo) }

// ─── Dashboard ────────────────────────────────────────────────────────────────

func TestDashboard_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.summary = Summary{Users: 42, Nurseries: 5}

	s, err := svc(repo).Dashboard(context.Background(), adminActor(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Users != 42 {
		t.Errorf("want 42 users, got %d", s.Users)
	}
}

func TestDashboard_SuperAdminSuccess(t *testing.T) {
	_, err := svc(newMock()).Dashboard(context.Background(), superActor(1))
	if err != nil {
		t.Fatalf("super admin should view dashboard: %v", err)
	}
}

func TestDashboard_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Dashboard(context.Background(), buyerActor(1))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestDashboard_OwnerForbidden(t *testing.T) {
	_, err := svc(newMock()).Dashboard(context.Background(), ownerActor(1))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for nursery owner, got %v", err)
	}
}

// ─── ListUsers ────────────────────────────────────────────────────────────────

func TestListUsers_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.users = []User{{ID: 1, FirstName: "Ravi"}, {ID: 2, FirstName: "Priya"}}

	users, _, err := svc(repo).ListUsers(context.Background(), adminActor(1), ListUsersRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("want 2 users, got %d", len(users))
	}
}

func TestListUsers_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).ListUsers(context.Background(), buyerActor(1), ListUsersRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── UpdateUserStatus ─────────────────────────────────────────────────────────

func TestUpdateUserStatus_AdminSuspends(t *testing.T) {
	repo := newMock()

	err := svc(repo).UpdateUserStatus(context.Background(), adminActor(1), 10, UpdateUserStatusRequest{Status: "SUSPENDED", Reason: "policy violation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.userStatuses[10] != "SUSPENDED" {
		t.Errorf("want SUSPENDED, got %s", repo.userStatuses[10])
	}
}

func TestUpdateUserStatus_AdminDeletes(t *testing.T) {
	repo := newMock()

	err := svc(repo).UpdateUserStatus(context.Background(), adminActor(1), 10, UpdateUserStatusRequest{Status: "DELETED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.userStatuses[10] != "DELETED" {
		t.Errorf("want DELETED, got %s", repo.userStatuses[10])
	}
}

func TestUpdateUserStatus_AdminActivates(t *testing.T) {
	repo := newMock()

	err := svc(repo).UpdateUserStatus(context.Background(), adminActor(1), 10, UpdateUserStatusRequest{Status: "ACTIVE"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.userStatuses[10] != "ACTIVE" {
		t.Errorf("want ACTIVE, got %s", repo.userStatuses[10])
	}
}

func TestUpdateUserStatus_BadStatusReturnsError(t *testing.T) {
	err := svc(newMock()).UpdateUserStatus(context.Background(), adminActor(1), 10, UpdateUserStatusRequest{Status: "BANNED"})
	if err == nil {
		t.Error("want error for bad status, got nil")
	}
}

func TestUpdateUserStatus_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).UpdateUserStatus(context.Background(), buyerActor(1), 10, UpdateUserStatusRequest{Status: "SUSPENDED"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── UpdateNurseryStatus ──────────────────────────────────────────────────────

func TestUpdateNurseryStatus_AdminSuccess(t *testing.T) {
	repo := newMock()

	err := svc(repo).UpdateNurseryStatus(context.Background(), adminActor(1), 5, UpdateNurseryStatusRequest{Status: "SUSPENDED", Reason: "policy violation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.nursStatuses[5] != "SUSPENDED" {
		t.Errorf("want SUSPENDED, got %s", repo.nursStatuses[5])
	}
}

func TestUpdateNurseryStatus_BadStatusReturnsError(t *testing.T) {
	err := svc(newMock()).UpdateNurseryStatus(context.Background(), adminActor(1), 5, UpdateNurseryStatusRequest{Status: "DELETED"})
	if err == nil {
		t.Error("want error for invalid nursery status DELETED, got nil")
	}
}

func TestUpdateNurseryStatus_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).UpdateNurseryStatus(context.Background(), buyerActor(1), 5, UpdateNurseryStatusRequest{Status: "ACTIVE"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// Reactivating a nursery must not lift a separate suspension of its owner.
func TestUpdateNurseryStatus_ReactivateKeepsSuspendedOwnerBlocked(t *testing.T) {
	repo := newMock()
	repo.ownerID = 77
	s := svc(repo)
	ctx := context.Background()
	if err := s.UpdateNurseryStatus(ctx, adminActor(1), 5, UpdateNurseryStatusRequest{Status: "SUSPENDED", Reason: "audit"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserStatus(ctx, adminActor(1), 77, UpdateUserStatusRequest{Status: "SUSPENDED", Reason: "fraud"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNurseryStatus(ctx, adminActor(1), 5, UpdateNurseryStatusRequest{Status: "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	if !revocation.IsRevoked(77) {
		t.Error("owner suspended in their own right must stay revoked after nursery reactivation")
	}
	if err := s.UpdateUserStatus(ctx, adminActor(1), 77, UpdateUserStatusRequest{Status: "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	if revocation.IsRevoked(77) {
		t.Error("reactivating the owner account should lift the block")
	}
}

// Reactivating the owner's account must not lift the block of a still-suspended nursery.
func TestUpdateUserStatus_ReactivatedOwnerOfSuspendedNurseryStaysBlocked(t *testing.T) {
	repo := newMock()
	repo.ownerID = 78
	s := svc(repo)
	ctx := context.Background()
	if err := s.UpdateNurseryStatus(ctx, adminActor(1), 5, UpdateNurseryStatusRequest{Status: "SUSPENDED", Reason: "audit"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserStatus(ctx, adminActor(1), 78, UpdateUserStatusRequest{Status: "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	if !revocation.IsRevoked(78) {
		t.Error("owner of a suspended nursery must stay blocked")
	}
	if err := s.UpdateNurseryStatus(ctx, adminActor(1), 5, UpdateNurseryStatusRequest{Status: "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	if revocation.IsRevoked(78) {
		t.Error("nursery reactivation should lift the owner block")
	}
}
