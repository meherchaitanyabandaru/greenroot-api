package topitems

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockRepo struct {
	items    map[int64]*TopItem
	owners   map[int64]int64 // nurseryID → owner userID
	managers map[int64]int64 // nurseryID → manager userID
	nextID   int64
}

func newMock() *mockRepo {
	return &mockRepo{items: map[int64]*TopItem{}, owners: map[int64]int64{1: 10, 2: 20}, managers: map[int64]int64{1: 30}, nextID: 100}
}

func (m *mockRepo) List(_ context.Context, _ ListRequest) ([]TopItem, int64, error) {
	out := make([]TopItem, 0, len(m.items))
	for _, it := range m.items {
		out = append(out, *it)
	}
	return out, int64(len(out)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*TopItem, error) {
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *it
	return &cp, nil
}

func (m *mockRepo) Create(_ context.Context, _ int64, in CreateRequest) (*TopItem, error) {
	m.nextID++
	it := &TopItem{ID: m.nextID, NurseryID: in.NurseryID, PlantID: in.PlantID, SizeID: in.SizeID, UnitPrice: in.UnitPrice}
	m.items[it.ID] = it
	return it, nil
}

func (m *mockRepo) Update(_ context.Context, _ int64, id int64, in UpdateRequest) (*TopItem, error) {
	it := m.items[id]
	it.PlantID, it.SizeID, it.UnitPrice = in.PlantID, in.SizeID, in.UnitPrice
	return it, nil
}

func (m *mockRepo) Delete(_ context.Context, id int64) error {
	delete(m.items, id)
	return nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.owners[nurseryID] == userID || m.managers[nurseryID] == userID, nil
}

func owner(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }
func admin() ActorContext         { return ActorContext{UserID: 1, Roles: []string{"ADMIN"}} }
func ptr[T any](v T) *T           { return &v }

func TestCreate_OwnerOnlyForOwnNursery(t *testing.T) {
	s := NewService(newMock(), nil)
	if _, err := s.Create(context.Background(), owner(10), CreateRequest{NurseryID: 1, PlantID: 5, SizeID: 2}); err != nil {
		t.Fatalf("own nursery: %v", err)
	}
	if _, err := s.Create(context.Background(), owner(10), CreateRequest{NurseryID: 2, PlantID: 5, SizeID: 2}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other nursery: want ErrForbidden, got %v", err)
	}
	manager := ActorContext{UserID: 30, Roles: []string{"MANAGER"}}
	if _, err := s.Create(context.Background(), manager, CreateRequest{NurseryID: 1, PlantID: 6, SizeID: 2}); err != nil {
		t.Fatalf("manager of the nursery: %v", err)
	}
	if _, err := s.Create(context.Background(), manager, CreateRequest{NurseryID: 2, PlantID: 6, SizeID: 2}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager of another nursery: want ErrForbidden, got %v", err)
	}
	if _, err := s.Create(context.Background(), admin(), CreateRequest{NurseryID: 2, PlantID: 5, SizeID: 2}); err != nil {
		t.Fatalf("admin: %v", err)
	}
}

func TestCreate_Validation(t *testing.T) {
	s := NewService(newMock(), nil)
	for name, in := range map[string]CreateRequest{
		"no plant":         {NurseryID: 1, SizeID: 2},
		"no size":          {NurseryID: 1, PlantID: 5},
		"negative price":   {NurseryID: 1, PlantID: 5, SizeID: 2, UnitPrice: ptr(-1.0)},
		"order too high":   {NurseryID: 1, PlantID: 5, SizeID: 2, DisplayOrder: ptr(int16(MaxPerNursery))},
		"negative order":   {NurseryID: 1, PlantID: 5, SizeID: 2, DisplayOrder: ptr(int16(-1))},
		"no nursery given": {PlantID: 5, SizeID: 2},
		"six photos":       {NurseryID: 1, PlantID: 5, SizeID: 2, Photos: []string{"a", "b", "c", "d", "e", "f"}},
		"blank photo":      {NurseryID: 1, PlantID: 5, SizeID: 2, Photos: []string{" "}},
		"notes too long":   {NurseryID: 1, PlantID: 5, SizeID: 2, QualityNotes: ptr(strings.Repeat("x", MaxNotesLength+1))},
	} {
		if _, err := s.Create(context.Background(), owner(10), in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}
}

// Permission comes from the item's own nursery, not from anything in the request.
func TestUpdateAndDelete_UseTheItemsNursery(t *testing.T) {
	repo := newMock()
	repo.items[7] = &TopItem{ID: 7, NurseryID: 2, PlantID: 5, SizeID: 2}
	s := NewService(repo, nil)
	if _, err := s.Update(context.Background(), owner(10), 7, UpdateRequest{PlantID: 6, SizeID: 2}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("update another nursery's item: want ErrForbidden, got %v", err)
	}
	if err := s.Delete(context.Background(), owner(10), 7); !errors.Is(err, ErrForbidden) {
		t.Fatalf("delete another nursery's item: want ErrForbidden, got %v", err)
	}
	if _, err := s.Update(context.Background(), owner(20), 7, UpdateRequest{PlantID: 6, SizeID: 2, UnitPrice: ptr(99.0)}); err != nil {
		t.Fatalf("owner update: %v", err)
	}
}

func TestList_CapabilitiesPerViewer(t *testing.T) {
	repo := newMock()
	repo.items[7] = &TopItem{ID: 7, NurseryID: 1}
	repo.items[8] = &TopItem{ID: 8, NurseryID: 2}
	items, _, err := NewService(repo, nil).List(context.Background(), owner(10), ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		want := it.NurseryID == 1
		if it.Capabilities == nil || it.Capabilities.CanEdit != want {
			t.Errorf("item of nursery %d: can_edit want %v, got %+v", it.NurseryID, want, it.Capabilities)
		}
	}
}
