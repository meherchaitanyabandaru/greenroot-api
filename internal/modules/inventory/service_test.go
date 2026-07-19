package inventory

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	items   map[int64]*InventoryItem
	members map[string]bool // "nurseryID:userID"
	nextID  int64
}

func newMock() *mockRepo {
	return &mockRepo{
		items:   make(map[int64]*InventoryItem),
		members: make(map[string]bool),
		nextID:  100,
	}
}

func mkKey(a, b int64) string { return fmt.Sprintf("%d:%d", a, b) }

func (m *mockRepo) seedMember(nurseryID, userID int64) {
	m.members[mkKey(nurseryID, userID)] = true
}

func (m *mockRepo) seedItem(id, nurseryID, plantID int64, sizeID int16, status string) *InventoryItem {
	item := &InventoryItem{ID: id, NurseryID: nurseryID, PlantID: plantID, SizeID: sizeID, Status: status}
	m.items[id] = item
	return item
}

// Repository interface

func (m *mockRepo) List(_ context.Context, _ ListInventoryRequest) ([]InventoryItem, int64, error) {
	result := make([]InventoryItem, 0, len(m.items))
	for _, item := range m.items {
		result = append(result, *item)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*InventoryItem, error) {
	item, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return item, nil
}

func (m *mockRepo) Create(_ context.Context, _ int64, input UpsertInventoryRequest) (*InventoryItem, error) {
	m.nextID++
	item := &InventoryItem{
		ID:                m.nextID,
		NurseryID:         input.NurseryID,
		PlantID:           input.PlantID,
		SizeID:            input.SizeID,
		AvailableQuantity: input.AvailableQuantity,
		Status:            input.Status,
	}
	m.items[m.nextID] = item
	return item, nil
}

func (m *mockRepo) Update(_ context.Context, _ int64, id int64, input UpsertInventoryRequest) (*InventoryItem, error) {
	item, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	item.AvailableQuantity = input.AvailableQuantity
	item.Status = input.Status
	return item, nil
}

func (m *mockRepo) Delete(_ context.Context, id int64) error {
	if _, ok := m.items[id]; !ok {
		return ErrNotFound
	}
	delete(m.items, id)
	return nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.members[mkKey(nurseryID, userID)], nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── List / Get ───────────────────────────────────────────────────────────────

func TestList_ReturnsAll(t *testing.T) {
	repo := newMock()
	repo.seedItem(1, 1, 5, 2, "AVAILABLE")
	repo.seedItem(2, 2, 5, 2, "AVAILABLE")

	items, _, err := svc(repo).List(context.Background(), ListInventoryRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("want 2 items, got %d", len(items))
	}
}

func TestGet_Found(t *testing.T) {
	repo := newMock()
	repo.seedItem(1, 1, 5, 2, "AVAILABLE")

	item, err := svc(repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID != 1 {
		t.Errorf("want ID 1, got %d", item.ID)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	item, err := svc(repo).Create(context.Background(), ownerActor(10), UpsertInventoryRequest{
		NurseryID:         1,
		PlantID:           5,
		SizeID:            2,
		AvailableQuantity: 50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Status != "AVAILABLE" {
		t.Errorf("default status should be AVAILABLE, got %s", item.Status)
	}
}

func TestCreate_AdminSuccess(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), UpsertInventoryRequest{
		NurseryID:         1,
		PlantID:           5,
		SizeID:            2,
		AvailableQuantity: 50,
	})
	if err != nil {
		t.Fatalf("admin should bypass member check: %v", err)
	}
}

func TestCreate_ManagerForbidden(t *testing.T) {
	// Manager is not in canManage — only NURSERY_OWNER and ADMIN
	repo := newMock()
	repo.seedMember(1, 20) // member but not owner role

	_, err := svc(repo).Create(context.Background(), managerActor(20), UpsertInventoryRequest{
		NurseryID: 1,
		PlantID:   5,
		SizeID:    2,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for manager, got %v", err)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), UpsertInventoryRequest{
		NurseryID: 1,
		PlantID:   5,
		SizeID:    2,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_NonMemberForbidden(t *testing.T) {
	repo := newMock()
	// user 10 is NOT a member of nursery 1

	_, err := svc(repo).Create(context.Background(), ownerActor(10), UpsertInventoryRequest{
		NurseryID: 1,
		PlantID:   5,
		SizeID:    2,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member, got %v", err)
	}
}

func TestCreate_MissingNurseryIDInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), UpsertInventoryRequest{
		PlantID: 5,
		SizeID:  2,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for missing NurseryID, got %v", err)
	}
}

func TestCreate_MissingPlantIDInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), UpsertInventoryRequest{
		NurseryID: 1,
		SizeID:    2,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for missing PlantID, got %v", err)
	}
}

func TestCreate_NegativeQuantityInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), UpsertInventoryRequest{
		NurseryID:         1,
		PlantID:           5,
		SizeID:            2,
		AvailableQuantity: -1,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for negative quantity, got %v", err)
	}
}

func TestCreate_InvalidStatusInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), UpsertInventoryRequest{
		NurseryID: 1,
		PlantID:   5,
		SizeID:    2,
		Status:    "BROKEN",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for invalid status, got %v", err)
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestUpdate_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedItem(1, 1, 5, 2, "AVAILABLE")

	item, err := svc(repo).Update(context.Background(), ownerActor(10), 1, UpsertInventoryRequest{
		NurseryID:         1,
		PlantID:           5,
		SizeID:            2,
		AvailableQuantity: 100,
		Status:            "LOW_STOCK",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Status != "LOW_STOCK" {
		t.Errorf("want LOW_STOCK, got %s", item.Status)
	}
}

func TestUpdate_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedItem(1, 1, 5, 2, "AVAILABLE")

	_, err := svc(repo).Update(context.Background(), buyerActor(1), 1, UpsertInventoryRequest{
		NurseryID: 1,
		PlantID:   5,
		SizeID:    2,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestDelete_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedItem(1, 1, 5, 2, "AVAILABLE")

	err := svc(repo).Delete(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.items[1]; ok {
		t.Error("item should have been deleted")
	}
}

func TestDelete_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedItem(1, 1, 5, 2, "AVAILABLE")

	err := svc(repo).Delete(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("admin should delete any item: %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	err := svc(newMock()).Delete(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Status allowed values table ─────────────────────────────────────────────

func TestIsAllowedStatus(t *testing.T) {
	allowed := []string{"AVAILABLE", "LOW_STOCK", "OUT_OF_STOCK", "RESERVED", "DISCONTINUED", ""}
	for _, s := range allowed {
		if !isAllowedStatus(s) {
			t.Errorf("status %q should be allowed", s)
		}
	}
	denied := []string{"DELETED", "BROKEN", "INACTIVE"}
	for _, s := range denied {
		if isAllowedStatus(s) {
			t.Errorf("status %q should NOT be allowed", s)
		}
	}
}
