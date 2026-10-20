package sourcing

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	members  map[int64]*Member // nurseryID → member
	isMember map[string]bool   // "nurseryID:userID"
	nextID   int64
}

func newMock() *mockRepo {
	return &mockRepo{
		members:  make(map[int64]*Member),
		isMember: make(map[string]bool),
		nextID:   100,
	}
}

func mkKey(a, b int64) string { return fmt.Sprintf("%d:%d", a, b) }

func (m *mockRepo) next() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedMember(nurseryID, userID int64) {
	m.isMember[mkKey(nurseryID, userID)] = true
}

func (m *mockRepo) GetMember(_ context.Context, nurseryID int64) (*Member, error) {
	mem, ok := m.members[nurseryID]
	if !ok {
		return nil, ErrNotFound
	}
	return mem, nil
}

func (m *mockRepo) JoinNetwork(_ context.Context, nurseryID, userID int64, req JoinNetworkRequest) (*Member, error) {
	mem := &Member{ID: m.next(), NurseryID: nurseryID, IsActive: true}
	m.members[nurseryID] = mem
	return mem, nil
}

func (m *mockRepo) LeaveNetwork(_ context.Context, nurseryID int64) error {
	delete(m.members, nurseryID)
	return nil
}

func (m *mockRepo) ListNearby(_ context.Context, q NearbyQuery) ([]NearbyNursery, int64, error) {
	return nil, 0, nil
}

func (m *mockRepo) GetNurseryProfile(_ context.Context, nurseryID int64) (*NearbyNursery, error) {
	nn := &NearbyNursery{NurseryID: nurseryID}
	return nn, nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.isMember[mkKey(nurseryID, userID)], nil
}

func (m *mockRepo) IsNurseryOwner(_ context.Context, nurseryID, userID int64) (bool, error) {
	return false, nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func driverActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── canSource gate ───────────────────────────────────────────────────────────

func TestListNearby_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).ListNearby(context.Background(), buyerActor(1), NearbyQuery{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestListNearby_DriverForbidden(t *testing.T) {
	_, _, err := svc(newMock()).ListNearby(context.Background(), driverActor(1), NearbyQuery{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for driver, got %v", err)
	}
}

func TestListNearby_OwnerSuccess(t *testing.T) {
	_, _, err := svc(newMock()).ListNearby(context.Background(), ownerActor(10), NearbyQuery{})
	if err != nil {
		t.Fatalf("owner should access sourcing: %v", err)
	}
}

func TestListNearby_ManagerSuccess(t *testing.T) {
	_, _, err := svc(newMock()).ListNearby(context.Background(), managerActor(10), NearbyQuery{})
	if err != nil {
		t.Fatalf("manager should access sourcing: %v", err)
	}
}

func TestGetNurseryProfile_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).GetNurseryProfile(context.Background(), buyerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── JoinNetwork / LeaveNetwork ──────────────────────────────────────────────

func TestJoinNetwork_MemberSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	mem, err := svc(repo).JoinNetwork(context.Background(), ownerActor(10), 1, JoinNetworkRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mem.NurseryID != 1 {
		t.Errorf("want nursery 1, got %d", mem.NurseryID)
	}
}

func TestJoinNetwork_AdminBypassesMembership(t *testing.T) {
	_, err := svc(newMock()).JoinNetwork(context.Background(), adminActor(1), 99, JoinNetworkRequest{})
	if err != nil {
		t.Fatalf("admin should bypass membership check: %v", err)
	}
}

func TestJoinNetwork_NonMemberForbidden(t *testing.T) {
	_, err := svc(newMock()).JoinNetwork(context.Background(), ownerActor(10), 1, JoinNetworkRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member, got %v", err)
	}
}

func TestLeaveNetwork_MemberSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	err := svc(repo).LeaveNetwork(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── CreatePost ──────────────────────────────────────────────────────────────

// ─── UpdatePost ──────────────────────────────────────────────────────────────

// ─── DeletePost ───────────────────────────────────────────────────────────────

// ─── CreateResponse ───────────────────────────────────────────────────────────

// ─── UpdateResponse ───────────────────────────────────────────────────────────
