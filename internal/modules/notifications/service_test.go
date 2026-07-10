package notifications

import (
	"context"
	"errors"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	notifications map[int64]*Notification
	devices       map[int64]*Device
	templates     map[int64]*Template
	nextID        int64
}

func newMock() *mockRepo {
	return &mockRepo{
		notifications: make(map[int64]*Notification),
		devices:       make(map[int64]*Device),
		templates:     make(map[int64]*Template),
		nextID:        100,
	}
}

func (m *mockRepo) nextID64() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedNotification(id int64, userID *int64, status string) *Notification {
	n := &Notification{ID: id, UserID: userID, Status: status, Type: "SYSTEM", Channel: "IN_APP"}
	m.notifications[id] = n
	return n
}

func (m *mockRepo) List(_ context.Context, input ListNotificationsRequest) ([]Notification, int64, error) {
	var result []Notification
	for _, n := range m.notifications {
		if input.UserID > 0 && (n.UserID == nil || *n.UserID != input.UserID) {
			continue
		}
		result = append(result, *n)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*Notification, error) {
	n, ok := m.notifications[id]
	if !ok {
		return nil, ErrNotFound
	}
	return n, nil
}

func (m *mockRepo) Create(_ context.Context, input CreateNotificationInput) (*Notification, error) {
	id := m.nextID64()
	n := &Notification{ID: id, UserID: input.UserID, Type: input.Type, Channel: input.Channel, Status: input.Status}
	m.notifications[id] = n
	return n, nil
}

func (m *mockRepo) MarkRead(_ context.Context, id int64) (*Notification, error) {
	n, ok := m.notifications[id]
	if !ok {
		return nil, ErrNotFound
	}
	n.Status = "READ"
	return n, nil
}

func (m *mockRepo) MarkAllRead(_ context.Context, userID int64) error { return nil }

func (m *mockRepo) Delete(_ context.Context, id int64) error {
	if _, ok := m.notifications[id]; !ok {
		return ErrNotFound
	}
	delete(m.notifications, id)
	return nil
}

func (m *mockRepo) ListDevices(_ context.Context, userID int64) ([]Device, error) {
	var result []Device
	for _, d := range m.devices {
		if d.UserID == userID {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (m *mockRepo) UpsertDevice(_ context.Context, userID int64, input DeviceRequest) (*Device, error) {
	id := m.nextID64()
	d := &Device{ID: id, UserID: userID, FCMToken: input.FCMToken, IsActive: true}
	m.devices[id] = d
	return d, nil
}

func (m *mockRepo) DeleteDevice(_ context.Context, id int64, userID int64, admin bool) error {
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	if !admin && d.UserID != userID {
		return ErrForbidden
	}
	delete(m.devices, id)
	return nil
}

func (m *mockRepo) ListTemplates(_ context.Context) ([]Template, error) {
	var result []Template
	for _, t := range m.templates {
		result = append(result, *t)
	}
	return result, nil
}

func (m *mockRepo) CreateTemplate(_ context.Context, input TemplateRequest) (*Template, error) {
	id := m.nextID64()
	t := &Template{ID: id, Code: input.Code, Channel: input.Channel, IsActive: input.IsActive}
	m.templates[id] = t
	return t, nil
}

func (m *mockRepo) UpdateTemplate(_ context.Context, id int64, input TemplateRequest) (*Template, error) {
	t, ok := m.templates[id]
	if !ok {
		return nil, ErrNotFound
	}
	t.Code = input.Code
	t.Channel = input.Channel
	return t, nil
}

func (m *mockRepo) DeleteTemplate(_ context.Context, id int64) error {
	if _, ok := m.templates[id]; !ok {
		return ErrNotFound
	}
	delete(m.templates, id)
	return nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func buyerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func ownerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }

func ptr[T any](v T) *T { return &v }

func svc(repo *mockRepo) *Service { return NewService(repo, MockSender{}, nil) }

// ─── List ────────────────────────────────────────────────────────────────────

func TestList_NonAdminSeesOnlyOwn(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")
	repo.seedNotification(2, ptr(int64(20)), "SENT")

	items, _, err := svc(repo).List(context.Background(), buyerActor(10), ListNotificationsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range items {
		if n.UserID == nil || *n.UserID != 10 {
			t.Errorf("non-admin should only see own notifications")
		}
	}
}

func TestList_AdminCanSeeAll(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")
	repo.seedNotification(2, ptr(int64(20)), "SENT")

	items, _, err := svc(repo).List(context.Background(), adminActor(1), ListNotificationsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("admin should see all 2 notifications, got %d", len(items))
	}
}

// ─── Get ─────────────────────────────────────────────────────────────────────

func TestGet_OwnNotification(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	n, err := svc(repo).Get(context.Background(), buyerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ID != 1 {
		t.Errorf("want ID 1, got %d", n.ID)
	}
}

func TestGet_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	_, err := svc(repo).Get(context.Background(), buyerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for other user, got %v", err)
	}
}

func TestGet_AdminCanReadAny(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	_, err := svc(repo).Get(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("admin should read any notification: %v", err)
	}
}

func TestGet_BroadcastForbiddenForNonAdmin(t *testing.T) {
	repo := newMock()
	// broadcast: UserID is nil
	repo.seedNotification(1, nil, "SENT")

	_, err := svc(repo).Get(context.Background(), buyerActor(10), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for broadcast notification accessed by buyer, got %v", err)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_AdminSuccess(t *testing.T) {
	n, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateNotificationRequest{
		UserID:  ptr(int64(10)),
		Type:    "SYSTEM",
		Channel: "IN_APP",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.ID == 0 {
		t.Error("want non-zero ID")
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(10), CreateNotificationRequest{
		UserID: ptr(int64(10)),
		Type:   "SYSTEM",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_InvalidTypeInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateNotificationRequest{
		UserID: ptr(int64(10)),
		Type:   "UNKNOWN_TYPE",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad type, got %v", err)
	}
}

func TestCreate_InvalidChannelInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateNotificationRequest{
		UserID:  ptr(int64(10)),
		Type:    "SYSTEM",
		Channel: "TELEGRAPH",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad channel, got %v", err)
	}
}

func TestCreate_DefaultsApplied(t *testing.T) {
	// Empty type/channel should default to SYSTEM/IN_APP
	n, err := svc(newMock()).Create(context.Background(), adminActor(1), CreateNotificationRequest{
		UserID: ptr(int64(10)),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Type != "SYSTEM" {
		t.Errorf("want default type SYSTEM, got %s", n.Type)
	}
	if n.Channel != "IN_APP" {
		t.Errorf("want default channel IN_APP, got %s", n.Channel)
	}
}

// ─── MarkRead / MarkAllRead ───────────────────────────────────────────────────

func TestMarkRead_OwnNotification(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	n, err := svc(repo).MarkRead(context.Background(), buyerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Status != "READ" {
		t.Errorf("want status READ, got %s", n.Status)
	}
}

func TestMarkRead_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	_, err := svc(repo).MarkRead(context.Background(), buyerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestMarkAllRead_Success(t *testing.T) {
	err := svc(newMock()).MarkAllRead(context.Background(), buyerActor(10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestDelete_OwnNotification(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	err := svc(repo).Delete(context.Background(), buyerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.notifications[1]; ok {
		t.Error("notification should have been deleted")
	}
}

func TestDelete_OtherUserForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	err := svc(repo).Delete(context.Background(), buyerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestDelete_AdminCanDeleteAny(t *testing.T) {
	repo := newMock()
	repo.seedNotification(1, ptr(int64(10)), "SENT")

	err := svc(repo).Delete(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("admin should delete any notification: %v", err)
	}
}

// ─── Devices ──────────────────────────────────────────────────────────────────

func TestUpsertDevice_Success(t *testing.T) {
	d, err := svc(newMock()).UpsertDevice(context.Background(), buyerActor(10), DeviceRequest{
		FCMToken: "abc123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.FCMToken != "abc123" {
		t.Errorf("want FCMToken abc123, got %s", d.FCMToken)
	}
}

func TestUpsertDevice_EmptyTokenInvalid(t *testing.T) {
	_, err := svc(newMock()).UpsertDevice(context.Background(), buyerActor(10), DeviceRequest{
		FCMToken: "   ",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty token, got %v", err)
	}
}

func TestListDevices_OwnDevices(t *testing.T) {
	repo := newMock()
	repo.devices[1] = &Device{ID: 1, UserID: 10, FCMToken: "abc"}
	repo.devices[2] = &Device{ID: 2, UserID: 20, FCMToken: "xyz"}

	devices, err := svc(repo).ListDevices(context.Background(), buyerActor(10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("want 1 device, got %d", len(devices))
	}
}

// ─── Templates (admin-only) ───────────────────────────────────────────────────

func TestListTemplates_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.templates[1] = &Template{ID: 1, Code: "WELCOME", Channel: "IN_APP"}

	templates, err := svc(repo).ListTemplates(context.Background(), adminActor(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(templates) != 1 {
		t.Errorf("want 1 template, got %d", len(templates))
	}
}

func TestListTemplates_OwnerForbidden(t *testing.T) {
	_, err := svc(newMock()).ListTemplates(context.Background(), ownerActor(1))
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for owner, got %v", err)
	}
}

func TestCreateTemplate_AdminSuccess(t *testing.T) {
	t_, err := svc(newMock()).CreateTemplate(context.Background(), adminActor(1), TemplateRequest{
		Code:    "WELCOME",
		Channel: "IN_APP",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if t_.Code != "WELCOME" {
		t.Errorf("want code WELCOME, got %s", t_.Code)
	}
}

func TestCreateTemplate_EmptyCodeInvalid(t *testing.T) {
	_, err := svc(newMock()).CreateTemplate(context.Background(), adminActor(1), TemplateRequest{
		Code:    "   ",
		Channel: "IN_APP",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty code, got %v", err)
	}
}

func TestCreateTemplate_InvalidChannelInvalid(t *testing.T) {
	_, err := svc(newMock()).CreateTemplate(context.Background(), adminActor(1), TemplateRequest{
		Code:    "WELCOME",
		Channel: "PIGEON_POST",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad channel, got %v", err)
	}
}

func TestCreateTemplate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).CreateTemplate(context.Background(), buyerActor(1), TemplateRequest{
		Code:    "WELCOME",
		Channel: "IN_APP",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestDeleteTemplate_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.templates[1] = &Template{ID: 1, Code: "WELCOME", Channel: "IN_APP"}

	err := svc(repo).DeleteTemplate(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteTemplate_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).DeleteTemplate(context.Background(), buyerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}
