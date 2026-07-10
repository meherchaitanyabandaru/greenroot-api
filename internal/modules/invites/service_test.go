package invites

import (
	"context"
	"errors"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	invites        map[string]*Invite // uuid → invite
	nurseryOwners  map[int64]bool     // userID → owns nursery
	nurseryOwnersN map[string]bool    // "nurseryID:userID" → is owner
	managers       map[int64]bool     // userID → is manager
	nextID         int64
}

func newMock() *mockRepo {
	return &mockRepo{
		invites:        make(map[string]*Invite),
		nurseryOwners:  make(map[int64]bool),
		nurseryOwnersN: make(map[string]bool),
		managers:       make(map[int64]bool),
		nextID:         100,
	}
}

func (m *mockRepo) next() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedInvite(uuid string, inviteType string, invitedByUserID int64) *Invite {
	inv := &Invite{ID: m.next(), InviteUUID: uuid, InviteType: inviteType, InvitedByUserID: invitedByUserID, Status: "PENDING"}
	m.invites[uuid] = inv
	return inv
}

func (m *mockRepo) Create(_ context.Context, actorID int64, req CreateInviteRequest) (*Invite, error) {
	id := m.next()
	inv := &Invite{ID: id, InviteUUID: "test-uuid", InviteType: req.InviteType, InvitedByUserID: actorID, Status: "PENDING",
		TargetMobile: req.TargetMobile, TargetEmail: req.TargetEmail, NurseryID: req.NurseryID}
	m.invites["test-uuid"] = inv
	return inv, nil
}

func (m *mockRepo) FindByUUID(_ context.Context, uuid string) (*Invite, error) {
	inv, ok := m.invites[uuid]
	if !ok {
		return nil, ErrNotFound
	}
	return inv, nil
}

func (m *mockRepo) Accept(_ context.Context, uuid string, acceptedByUserID int64) (*Invite, error) {
	inv, ok := m.invites[uuid]
	if !ok {
		return nil, ErrNotFound
	}
	inv.Status = "ACCEPTED"
	inv.AcceptedByUserID = &acceptedByUserID
	return inv, nil
}

func (m *mockRepo) Cancel(_ context.Context, uuid string, actorID int64) (*Invite, error) {
	inv, ok := m.invites[uuid]
	if !ok {
		return nil, ErrNotFound
	}
	inv.Status = "CANCELLED"
	return inv, nil
}

func (m *mockRepo) ListByNursery(_ context.Context, nurseryID int64) ([]Invite, error) {
	var result []Invite
	for _, inv := range m.invites {
		if inv.NurseryID != nil && *inv.NurseryID == nurseryID {
			result = append(result, *inv)
		}
	}
	return result, nil
}

func (m *mockRepo) ListAcceptedByUser(_ context.Context, userID int64) ([]Invite, error) {
	var result []Invite
	for _, inv := range m.invites {
		if inv.AcceptedByUserID != nil && *inv.AcceptedByUserID == userID {
			result = append(result, *inv)
		}
	}
	return result, nil
}

func (m *mockRepo) UserOwnsNursery(_ context.Context, userID int64) (bool, error) {
	return m.nurseryOwners[userID], nil
}

func (m *mockRepo) IsNurseryOwner(_ context.Context, nurseryID int64, userID int64) (bool, error) {
	key := nurseryMemberKey(nurseryID, userID)
	return m.nurseryOwnersN[key], nil
}

func nurseryMemberKey(nurseryID, userID int64) string {
	return string(rune('0'+nurseryID)) + ":" + string(rune('0'+userID))
}

func (m *mockRepo) UserIsManager(_ context.Context, userID int64) (bool, error) {
	return m.managers[userID], nil
}

func (m *mockRepo) AddNurseryMember(_ context.Context, nurseryID, userID int64, role string, invitedByUserID int64) error {
	return nil
}

func (m *mockRepo) GrantNurseryOwnerRole(_ context.Context, userID int64) error {
	m.nurseryOwners[userID] = true
	return nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func ptr[T any](v T) *T { return &v }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_OwnerCanInviteManager(t *testing.T) {
	inv, err := svc(newMock()).Create(context.Background(), ownerActor(10), CreateInviteRequest{
		InviteType:   "MANAGER_INVITE",
		TargetMobile: ptr("9100000000"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.InviteType != "MANAGER_INVITE" {
		t.Errorf("want MANAGER_INVITE, got %s", inv.InviteType)
	}
}

func TestCreate_ManagerCanInviteDriver(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), managerActor(10), CreateInviteRequest{
		InviteType:   "DRIVER_INVITE",
		TargetMobile: ptr("9400000000"),
	})
	if err != nil {
		t.Fatalf("manager should invite driver: %v", err)
	}
}

func TestCreate_ManagerCannotInviteManager(t *testing.T) {
	// Only ADMIN/SUPER_ADMIN/NURSERY_OWNER may create MANAGER_INVITE
	_, err := svc(newMock()).Create(context.Background(), managerActor(10), CreateInviteRequest{
		InviteType:   "MANAGER_INVITE",
		TargetMobile: ptr("9100000000"),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for manager creating manager invite, got %v", err)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), CreateInviteRequest{
		InviteType:   "DRIVER_INVITE",
		TargetMobile: ptr("9400000000"),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_OnlyAdminCanInviteNursery(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), ownerActor(10), CreateInviteRequest{
		InviteType:   "NURSERY_ONBOARDING_INVITE",
		TargetMobile: ptr("9100000000"),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for owner creating nursery onboarding invite, got %v", err)
	}
}

func TestCreate_AdminCanInviteNursery(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateInviteRequest{
		InviteType:   "NURSERY_ONBOARDING_INVITE",
		TargetMobile: ptr("9100000000"),
	})
	if err != nil {
		t.Fatalf("admin should create nursery onboarding invite: %v", err)
	}
}

func TestCreate_BadInviteTypeInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateInviteRequest{
		InviteType:   "UNKNOWN_INVITE",
		TargetMobile: ptr("9100000000"),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad type, got %v", err)
	}
}

func TestCreate_NoContactInvalid(t *testing.T) {
	// Neither mobile nor email set
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateInviteRequest{
		InviteType: "DRIVER_INVITE",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput when no contact provided, got %v", err)
	}
}

func TestCreate_EmailContactAccepted(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateInviteRequest{
		InviteType:  "DRIVER_INVITE",
		TargetEmail: ptr("driver@example.com"),
	})
	if err != nil {
		t.Fatalf("email contact should be accepted: %v", err)
	}
}

// ─── Accept ──────────────────────────────────────────────────────────────────

func TestAccept_Success(t *testing.T) {
	repo := newMock()
	repo.seedInvite("uuid-1", "DRIVER_INVITE", 1)

	inv, err := svc(repo).Accept(context.Background(), buyerActor(20), "uuid-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != "ACCEPTED" {
		t.Errorf("want ACCEPTED, got %s", inv.Status)
	}
}

func TestAccept_ManagerInvite_OwnerConflict(t *testing.T) {
	repo := newMock()
	repo.seedInvite("uuid-2", "MANAGER_INVITE", 1)
	repo.nurseryOwners[20] = true // user 20 already owns a nursery

	_, err := svc(repo).Accept(context.Background(), ownerActor(20), "uuid-2")
	if !errors.Is(err, ErrConflictingRole) {
		t.Errorf("want ErrConflictingRole for nursery owner accepting manager invite, got %v", err)
	}
}

func TestAccept_NurseryOnboarding_ManagerConflict(t *testing.T) {
	repo := newMock()
	repo.seedInvite("uuid-3", "NURSERY_ONBOARDING_INVITE", 1)
	repo.managers[20] = true // user 20 is already a manager

	_, err := svc(repo).Accept(context.Background(), managerActor(20), "uuid-3")
	if !errors.Is(err, ErrConflictingRole) {
		t.Errorf("want ErrConflictingRole for manager accepting nursery invite, got %v", err)
	}
}

func TestAccept_NotFound(t *testing.T) {
	_, err := svc(newMock()).Accept(context.Background(), buyerActor(20), "no-such-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Cancel ──────────────────────────────────────────────────────────────────

func TestCancel_InviterCanCancel(t *testing.T) {
	repo := newMock()
	repo.seedInvite("uuid-4", "DRIVER_INVITE", 10) // invited by user 10

	inv, err := svc(repo).Cancel(context.Background(), ownerActor(10), "uuid-4")
	if err != nil {
		t.Fatalf("inviter should cancel own invite: %v", err)
	}
	if inv.Status != "CANCELLED" {
		t.Errorf("want CANCELLED, got %s", inv.Status)
	}
}

func TestCancel_AdminCanCancelAny(t *testing.T) {
	repo := newMock()
	repo.seedInvite("uuid-5", "DRIVER_INVITE", 10)

	_, err := svc(repo).Cancel(context.Background(), adminActor(1), "uuid-5")
	if err != nil {
		t.Fatalf("admin should cancel any invite: %v", err)
	}
}

func TestCancel_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedInvite("uuid-6", "DRIVER_INVITE", 10) // invited by user 10

	_, err := svc(repo).Cancel(context.Background(), buyerActor(20), "uuid-6") // different user
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-inviter, got %v", err)
	}
}

// ─── GetByUUID ───────────────────────────────────────────────────────────────

func TestGetByUUID_Found(t *testing.T) {
	repo := newMock()
	repo.seedInvite("uuid-7", "CUSTOMER_INVITE", 1)

	inv, err := svc(repo).GetByUUID(context.Background(), "uuid-7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.InviteType != "CUSTOMER_INVITE" {
		t.Errorf("want CUSTOMER_INVITE, got %s", inv.InviteType)
	}
}

func TestGetByUUID_NotFound(t *testing.T) {
	_, err := svc(newMock()).GetByUUID(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
