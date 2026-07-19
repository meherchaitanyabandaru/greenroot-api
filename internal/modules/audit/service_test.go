package audit

import (
	"context"
	"errors"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	logs    []AuditLog
	secLogs []SecurityLog
}

func newMock() *mockRepo { return &mockRepo{} }

func (m *mockRepo) List(_ context.Context, _ ListRequest) ([]AuditLog, int64, error) {
	return m.logs, int64(len(m.logs)), nil
}

func (m *mockRepo) ListSecurity(_ context.Context, _ ListSecurityRequest) ([]SecurityLog, int64, error) {
	return m.secLogs, int64(len(m.secLogs)), nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func buyerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}

func svc(repo *mockRepo) *Service { return NewService(repo) }

// ─── List (admin-only) ────────────────────────────────────────────────────────

func TestList_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.logs = []AuditLog{{ID: 1}, {ID: 2}}

	logs, _, err := svc(repo).List(context.Background(), adminActor(1), ListRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("want 2 logs, got %d", len(logs))
	}
}

func TestList_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), buyerActor(1), ListRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestList_OwnerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).List(context.Background(), ownerActor(1), ListRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for nursery owner, got %v", err)
	}
}

func TestList_EmptyResult(t *testing.T) {
	logs, _, err := svc(newMock()).List(context.Background(), adminActor(1), ListRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("want 0 logs, got %d", len(logs))
	}
}

func TestList_PaginationDefaults(t *testing.T) {
	_, pg, err := svc(newMock()).List(context.Background(), adminActor(1), ListRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pg.Page != 1 {
		t.Errorf("want page 1, got %d", pg.Page)
	}
	if pg.PerPage != 20 {
		t.Errorf("want per_page 20, got %d", pg.PerPage)
	}
}

// ─── ListSecurity (admin-only) ────────────────────────────────────────────────

func TestListSecurity_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.secLogs = []SecurityLog{{ID: 1}}

	logs, _, err := svc(repo).ListSecurity(context.Background(), adminActor(1), ListSecurityRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("want 1 security log, got %d", len(logs))
	}
}

func TestListSecurity_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).ListSecurity(context.Background(), buyerActor(1), ListSecurityRequest{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}
