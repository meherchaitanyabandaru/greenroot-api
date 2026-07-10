package attachments

import (
	"context"
	"errors"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	items  map[int64]*Attachment
	nextID int64
}

func newMock() *mockRepo {
	return &mockRepo{items: make(map[int64]*Attachment), nextID: 100}
}

func (m *mockRepo) next() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedAttachment(id int64, entityType string, entityID int64) *Attachment {
	a := &Attachment{ID: id, EntityType: entityType, EntityID: entityID, FileName: "test.pdf", FileURL: "https://x.com/t.pdf"}
	m.items[id] = a
	return a
}

func (m *mockRepo) List(_ context.Context, _ ListRequest) ([]Attachment, int64, error) {
	result := make([]Attachment, 0, len(m.items))
	for _, a := range m.items {
		result = append(result, *a)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*Attachment, error) {
	a, ok := m.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return a, nil
}

func (m *mockRepo) Create(_ context.Context, actorID int64, input AttachmentRequest) (*Attachment, error) {
	id := m.next()
	a := &Attachment{ID: id, EntityType: input.EntityType, EntityID: input.EntityID, FileName: input.FileName, FileURL: input.FileURL, UploadedBy: &actorID}
	m.items[id] = a
	return a, nil
}

func (m *mockRepo) Delete(_ context.Context, id int64) error {
	if _, ok := m.items[id]; !ok {
		return errors.New("not found")
	}
	delete(m.items, id)
	return nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func driverActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo) }

// ─── List / Get ───────────────────────────────────────────────────────────────

func TestList_AnyRoleCanList(t *testing.T) {
	repo := newMock()
	repo.seedAttachment(1, "ORDER", 5)

	items, _, err := svc(repo).List(context.Background(), buyerActor(1), ListRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("want 1 item, got %d", len(items))
	}
}

func TestGet_Found(t *testing.T) {
	repo := newMock()
	repo.seedAttachment(1, "ORDER", 5)

	a, err := svc(repo).Get(context.Background(), buyerActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.EntityType != "ORDER" {
		t.Errorf("want ORDER, got %s", a.EntityType)
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_AdminSuccess(t *testing.T) {
	a, err := svc(newMock()).Create(context.Background(), adminActor(1), AttachmentRequest{
		EntityType: "ORDER",
		EntityID:   5,
		FileName:   "invoice.pdf",
		FileURL:    "https://example.com/invoice.pdf",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.FileName != "invoice.pdf" {
		t.Errorf("want invoice.pdf, got %s", a.FileName)
	}
}

func TestCreate_OwnerSuccess(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), ownerActor(10), AttachmentRequest{
		EntityType: "DISPATCH",
		EntityID:   3,
		FileName:   "doc.pdf",
		FileURL:    "https://example.com/doc.pdf",
	})
	if err != nil {
		t.Fatalf("nursery owner should upload: %v", err)
	}
}

func TestCreate_ManagerSuccess(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), managerActor(10), AttachmentRequest{
		EntityType: "ORDER",
		EntityID:   1,
		FileName:   "bill.pdf",
		FileURL:    "https://example.com/bill.pdf",
	})
	if err != nil {
		t.Fatalf("manager should upload: %v", err)
	}
}

func TestCreate_DriverSuccess(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), driverActor(10), AttachmentRequest{
		EntityType: "DISPATCH",
		EntityID:   2,
		FileName:   "photo.jpg",
		FileURL:    "https://example.com/photo.jpg",
	})
	if err != nil {
		t.Fatalf("driver should upload: %v", err)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), AttachmentRequest{
		EntityType: "ORDER",
		EntityID:   5,
		FileName:   "hack.pdf",
		FileURL:    "https://example.com/hack.pdf",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_EmptyEntityTypeInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), AttachmentRequest{
		EntityType: "   ",
		EntityID:   5,
		FileName:   "doc.pdf",
		FileURL:    "https://example.com/doc.pdf",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty entity type, got %v", err)
	}
}

func TestCreate_ZeroEntityIDInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), AttachmentRequest{
		EntityType: "ORDER",
		EntityID:   0,
		FileName:   "doc.pdf",
		FileURL:    "https://example.com/doc.pdf",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for EntityID=0, got %v", err)
	}
}

func TestCreate_EmptyFileNameInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), AttachmentRequest{
		EntityType: "ORDER",
		EntityID:   5,
		FileName:   "   ",
		FileURL:    "https://example.com/doc.pdf",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty file name, got %v", err)
	}
}

func TestCreate_EmptyFileURLInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), AttachmentRequest{
		EntityType: "ORDER",
		EntityID:   5,
		FileName:   "doc.pdf",
		FileURL:    "   ",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty file URL, got %v", err)
	}
}

// ─── Delete (admin-only) ──────────────────────────────────────────────────────

func TestDelete_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedAttachment(1, "ORDER", 5)

	err := svc(repo).Delete(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.items[1]; ok {
		t.Error("attachment should have been deleted")
	}
}

func TestDelete_OwnerForbidden(t *testing.T) {
	err := svc(newMock()).Delete(context.Background(), ownerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for nursery owner, got %v", err)
	}
}

func TestDelete_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).Delete(context.Background(), buyerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}
