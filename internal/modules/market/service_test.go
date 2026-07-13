package market

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	ads           map[int64]Ad
	nurseries     map[int64]bool  // nurseryID → active
	members       map[string]bool // "nurseryID:userID" → member
	userNursery   map[int64]int64 // userID → nurseryID
	enquiries     map[int64]Enquiry
	saves         map[string]bool // "adID:nurseryID" → saved
	enquiryExists map[string]bool // "adID:nurseryID"
	nextAdID      int64
	nextEnqID     int64
}

func newMock() *mockRepo {
	return &mockRepo{
		ads:           make(map[int64]Ad),
		nurseries:     make(map[int64]bool),
		members:       make(map[string]bool),
		userNursery:   make(map[int64]int64),
		enquiries:     make(map[int64]Enquiry),
		saves:         make(map[string]bool),
		enquiryExists: make(map[string]bool),
		nextAdID:      100,
		nextEnqID:     200,
	}
}

func mk(a, b int64) string { return fmt.Sprintf("%d:%d", a, b) }

func (m *mockRepo) seedNursery(nurseryID int64, active bool, userIDs ...int64) {
	m.nurseries[nurseryID] = active
	for _, uid := range userIDs {
		m.members[mk(nurseryID, uid)] = true
		m.userNursery[uid] = nurseryID
	}
}

func (m *mockRepo) seedAd(id, nurseryID int64, status string) Ad {
	ad := Ad{ID: id, NurseryID: nurseryID, Status: status, Title: "Test Ad", PlantName: "Rose"}
	m.ads[id] = ad
	return ad
}

func (m *mockRepo) seedEnquiry(id, adID, adNurseryID, enquiringNurseryID int64, status string) Enquiry {
	e := Enquiry{
		ID:                 id,
		AdID:               adID,
		AdNurseryID:        adNurseryID,
		EnquiringNurseryID: enquiringNurseryID,
		Status:             status,
		Message:            "Hello",
	}
	m.enquiries[id] = e
	m.enquiryExists[mk(adID, enquiringNurseryID)] = true
	return e
}

// Repository interface implementation

func (m *mockRepo) NurseryIDForUser(_ context.Context, userID int64) (int64, error) {
	nid, ok := m.userNursery[userID]
	if !ok || nid == 0 {
		return 0, ErrNotFound
	}
	return nid, nil
}

func (m *mockRepo) NurseryName(_ context.Context, _ int64) (string, error) { return "Test", nil }

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.members[mk(nurseryID, userID)], nil
}

func (m *mockRepo) IsNurseryActive(_ context.Context, nurseryID int64) (bool, error) {
	active, ok := m.nurseries[nurseryID]
	if !ok {
		return false, ErrNotFound
	}
	return active, nil
}

func (m *mockRepo) CreateAd(_ context.Context, nurseryID, userID int64, req CreateAdRequest) (Ad, error) {
	m.nextAdID++
	ad := Ad{ID: m.nextAdID, NurseryID: nurseryID, Title: req.Title, PlantName: req.PlantName, Status: StatusDraft}
	m.ads[m.nextAdID] = ad
	return ad, nil
}

func (m *mockRepo) GetAd(_ context.Context, id int64) (Ad, error) {
	ad, ok := m.ads[id]
	if !ok {
		return Ad{}, ErrNotFound
	}
	return ad, nil
}

func (m *mockRepo) ListPublished(_ context.Context, _ AdsQuery) ([]Ad, int, error) {
	result := []Ad{}
	for _, a := range m.ads {
		if a.Status == StatusPublished {
			result = append(result, a)
		}
	}
	return result, len(result), nil
}

func (m *mockRepo) ListByNursery(_ context.Context, nurseryID int64, _ AdsQuery) ([]Ad, int, error) {
	result := []Ad{}
	for _, a := range m.ads {
		if a.NurseryID == nurseryID {
			result = append(result, a)
		}
	}
	return result, len(result), nil
}

func (m *mockRepo) UpdateAd(_ context.Context, id, _ int64, req UpdateAdRequest) (Ad, error) {
	ad, ok := m.ads[id]
	if !ok {
		return Ad{}, ErrNotFound
	}
	if req.Title != nil {
		ad.Title = *req.Title
	}
	m.ads[id] = ad
	return ad, nil
}

func (m *mockRepo) SetAdStatus(_ context.Context, id int64, status string, _ *time.Time) error {
	ad, ok := m.ads[id]
	if !ok {
		return ErrNotFound
	}
	ad.Status = status
	m.ads[id] = ad
	return nil
}

func (m *mockRepo) SetExpiry(_ context.Context, _ int64, _ time.Time) error { return nil }

func (m *mockRepo) RecordView(_ context.Context, _ int64, _ int64) error { return nil }

func (m *mockRepo) ToggleSave(_ context.Context, adID, nurseryID, _ int64) (bool, error) {
	key := mk(adID, nurseryID)
	m.saves[key] = !m.saves[key]
	return m.saves[key], nil
}

func (m *mockRepo) FlushAdCounters(_ context.Context, views map[int64]int64, saves map[int64]int64) error {
	for adID, delta := range views {
		ad := m.ads[adID]
		ad.ViewCount += int(delta)
		m.ads[adID] = ad
	}
	for adID, delta := range saves {
		ad := m.ads[adID]
		ad.SaveCount += int(delta)
		if ad.SaveCount < 0 {
			ad.SaveCount = 0
		}
		m.ads[adID] = ad
	}
	return nil
}

func (m *mockRepo) IsSaved(_ context.Context, adID, nurseryID int64) (bool, error) {
	return m.saves[mk(adID, nurseryID)], nil
}

func (m *mockRepo) ListSaved(_ context.Context, nurseryID int64, _ AdsQuery) ([]Ad, int, error) {
	result := []Ad{}
	for adID, saved := range m.saves {
		if !saved {
			continue
		}
		var aid int64
		fmt.Sscanf(adID, "%d:", &aid)
		if ad, ok := m.ads[aid]; ok && m.saves[mk(aid, nurseryID)] {
			result = append(result, ad)
		}
	}
	return result, len(result), nil
}

func (m *mockRepo) CreateReport(_ context.Context, _ int64, _ int64, _ int64, _ string, _ *string) error {
	return nil
}

func (m *mockRepo) CreateEnquiry(_ context.Context, adID, adNurseryID, enquiringNurseryID, userID int64, req CreateEnquiryRequest) (Enquiry, error) {
	m.nextEnqID++
	e := Enquiry{
		ID:                 m.nextEnqID,
		AdID:               adID,
		AdNurseryID:        adNurseryID,
		EnquiringNurseryID: enquiringNurseryID,
		CreatedByUserID:    userID,
		Message:            req.Message,
		Status:             EnquiryNew,
	}
	m.enquiries[m.nextEnqID] = e
	m.enquiryExists[mk(adID, enquiringNurseryID)] = true
	return e, nil
}

func (m *mockRepo) GetEnquiry(_ context.Context, id int64) (Enquiry, error) {
	e, ok := m.enquiries[id]
	if !ok {
		return Enquiry{}, ErrNotFound
	}
	return e, nil
}

func (m *mockRepo) ListEnquiries(_ context.Context, nurseryID int64, _ EnquiriesQuery) ([]Enquiry, int, error) {
	result := []Enquiry{}
	for _, e := range m.enquiries {
		if e.AdNurseryID == nurseryID || e.EnquiringNurseryID == nurseryID {
			result = append(result, e)
		}
	}
	return result, len(result), nil
}

func (m *mockRepo) MarkEnquiryViewed(_ context.Context, _ int64) error { return nil }

func (m *mockRepo) AddMessage(_ context.Context, enquiryID, userID, nurseryID int64, body string) (Message, error) {
	return Message{ID: 1, EnquiryID: enquiryID, SentByUserID: userID, Body: body}, nil
}

func (m *mockRepo) SetEnquiryStatus(_ context.Context, id int64, status string) error {
	e, ok := m.enquiries[id]
	if !ok {
		return ErrNotFound
	}
	e.Status = status
	m.enquiries[id] = e
	return nil
}

func (m *mockRepo) LinkQuotation(_ context.Context, enquiryID, quotationID int64) error {
	e, ok := m.enquiries[enquiryID]
	if !ok {
		return ErrNotFound
	}
	e.QuotationID = &quotationID
	m.enquiries[enquiryID] = e
	return nil
}

func (m *mockRepo) GetMessages(_ context.Context, _ int64) ([]Message, error) { return nil, nil }

func (m *mockRepo) HasEnquiry(_ context.Context, adID, nurseryID int64) (bool, error) {
	return m.enquiryExists[mk(adID, nurseryID)], nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func adminActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo) }

// ─── CreateAd ────────────────────────────────────────────────────────────────

func TestCreateAd_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)

	ad, err := svc(repo).CreateAd(context.Background(), ownerActor(10), CreateAdRequest{
		Title:     "Rose Saplings",
		PlantName: "Rose",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ad.Status != StatusDraft {
		t.Errorf("new ad should be DRAFT, got %s", ad.Status)
	}
}

func TestCreateAd_ManagerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 20)

	_, err := svc(repo).CreateAd(context.Background(), managerActor(20), CreateAdRequest{
		Title:     "Tulsi Plants",
		PlantName: "Tulsi",
	})
	if err != nil {
		t.Fatalf("manager should be able to create ads: %v", err)
	}
}

func TestCreateAd_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).CreateAd(context.Background(), buyerActor(10), CreateAdRequest{
		Title:     "Rose",
		PlantName: "Rose",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreateAd_AdminForbidden(t *testing.T) {
	// Admin has no nursery context → actorNurseryID returns error → ErrForbidden
	_, err := svc(newMock()).CreateAd(context.Background(), adminActor(1), CreateAdRequest{
		Title:     "Rose",
		PlantName: "Rose",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for admin (no nursery context), got %v", err)
	}
}

func TestCreateAd_InactiveNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, false, 10) // nursery not active

	_, err := svc(repo).CreateAd(context.Background(), ownerActor(10), CreateAdRequest{
		Title:     "Rose",
		PlantName: "Rose",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for inactive nursery, got %v", err)
	}
}

func TestCreateAd_MissingTitle(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)

	_, err := svc(repo).CreateAd(context.Background(), ownerActor(10), CreateAdRequest{PlantName: "Rose"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for missing title, got %v", err)
	}
}

func TestCreateAd_TooManyPhotos(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)

	photos := make([]string, 11)
	_, err := svc(repo).CreateAd(context.Background(), ownerActor(10), CreateAdRequest{
		Title:     "Rose",
		PlantName: "Rose",
		Photos:    photos,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for too many photos, got %v", err)
	}
}

// ─── GetAd ───────────────────────────────────────────────────────────────────

func TestGetAd_PublishedVisibleToOthers(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.seedAd(1, 1, StatusPublished)

	ad, err := svc(repo).GetAd(context.Background(), ownerActor(20), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ad.ID != 1 {
		t.Errorf("want ad ID 1, got %d", ad.ID)
	}
}

func TestGetAd_DraftNotVisibleToOthers(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.seedAd(1, 1, StatusDraft)

	_, err := svc(repo).GetAd(context.Background(), ownerActor(20), 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound for draft ad from another nursery, got %v", err)
	}
}

func TestGetAd_OwnerCanSeeDraft(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusDraft)

	ad, err := svc(repo).GetAd(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("owner should see own draft: %v", err)
	}
	if ad.Status != StatusDraft {
		t.Errorf("want DRAFT, got %s", ad.Status)
	}
}

func TestGetAd_NotFound(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)

	_, err := svc(repo).GetAd(context.Background(), ownerActor(10), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Ad status transitions ────────────────────────────────────────────────────

func TestPublishAd_FromDraft(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusDraft)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).PublishAd(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.ads[1].Status != StatusPublished {
		t.Errorf("want PUBLISHED, got %s", repo.ads[1].Status)
	}
}

func TestPublishAd_FromPublished_InvalidTransition(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusPublished)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).PublishAd(context.Background(), ownerActor(10), 1)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for publish→publish, got %v", err)
	}
}

func TestPauseAd_FromPublished(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusPublished)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).PauseAd(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.ads[1].Status != StatusPaused {
		t.Errorf("want PAUSED, got %s", repo.ads[1].Status)
	}
}

func TestPauseAd_FromDraft_InvalidTransition(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusDraft)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).PauseAd(context.Background(), ownerActor(10), 1)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for draft→pause, got %v", err)
	}
}

func TestResumeAd_FromPaused(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusPaused)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).ResumeAd(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestArchiveAd_FromPublished(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusPublished)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).ArchiveAd(context.Background(), ownerActor(10), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.ads[1].Status != StatusArchived {
		t.Errorf("want ARCHIVED, got %s", repo.ads[1].Status)
	}
}

func TestAdTransition_OtherNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad belongs to nursery 1
	repo.seedNursery(2, true, 20) // actor is nursery 2
	repo.seedAd(1, 1, StatusDraft)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).PublishAd(context.Background(), ownerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for other nursery, got %v", err)
	}
}

func TestUpdateAd_ArchivedRejected(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusArchived)
	repo.members[mk(1, 10)] = true

	_, err := svc(repo).UpdateAd(context.Background(), ownerActor(10), 1, UpdateAdRequest{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for archived ad, got %v", err)
	}
}

// ─── ToggleSave ──────────────────────────────────────────────────────────────

func TestToggleSave_Success(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad owner
	repo.seedNursery(2, true, 20) // saver
	repo.seedAd(1, 1, StatusPublished)

	saved, err := svc(repo).ToggleSave(context.Background(), ownerActor(20), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !saved {
		t.Error("expected saved=true on first toggle")
	}
}

func TestToggleSave_OwnAdForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusPublished)

	_, err := svc(repo).ToggleSave(context.Background(), ownerActor(10), 1)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for saving own ad, got %v", err)
	}
}

// ─── ReportAd ────────────────────────────────────────────────────────────────

func TestReportAd_ValidReason(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad owner
	repo.seedNursery(2, true, 20) // reporter
	repo.seedAd(1, 1, StatusPublished)

	err := svc(repo).ReportAd(context.Background(), ownerActor(20), 1, ReportAdRequest{Reason: "SPAM"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReportAd_InvalidReason(t *testing.T) {
	repo := newMock()
	repo.seedNursery(2, true, 20)
	repo.seedAd(1, 1, StatusPublished)

	err := svc(repo).ReportAd(context.Background(), ownerActor(20), 1, ReportAdRequest{Reason: "BAD"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad reason, got %v", err)
	}
}

func TestReportAd_OwnAdForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusPublished)

	err := svc(repo).ReportAd(context.Background(), ownerActor(10), 1, ReportAdRequest{Reason: "SPAM"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for reporting own ad, got %v", err)
	}
}

func TestReportAd_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).ReportAd(context.Background(), buyerActor(10), 1, ReportAdRequest{Reason: "SPAM"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

// ─── SendEnquiry ─────────────────────────────────────────────────────────────

func TestSendEnquiry_Success(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad nursery
	repo.seedNursery(2, true, 20) // enquiring nursery
	repo.seedAd(1, 1, StatusPublished)

	e, err := svc(repo).SendEnquiry(context.Background(), ownerActor(20), 1, CreateEnquiryRequest{Message: "I am interested"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Status != EnquiryNew {
		t.Errorf("want NEW, got %s", e.Status)
	}
}

func TestSendEnquiry_OwnAdForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusPublished)

	_, err := svc(repo).SendEnquiry(context.Background(), ownerActor(10), 1, CreateEnquiryRequest{Message: "test"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for own ad, got %v", err)
	}
}

func TestSendEnquiry_UnpublishedAdForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad nursery
	repo.seedNursery(2, true, 20) // enquiring nursery
	repo.seedAd(1, 1, StatusDraft)

	_, err := svc(repo).SendEnquiry(context.Background(), ownerActor(20), 1, CreateEnquiryRequest{Message: "test"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for unpublished ad, got %v", err)
	}
}

func TestSendEnquiry_DuplicateForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.seedAd(1, 1, StatusPublished)
	repo.enquiryExists[mk(1, 2)] = true // already enquired

	_, err := svc(repo).SendEnquiry(context.Background(), ownerActor(20), 1, CreateEnquiryRequest{Message: "again"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for duplicate enquiry, got %v", err)
	}
}

func TestSendEnquiry_EmptyMessageForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(2, true, 20)
	repo.seedAd(1, 1, StatusPublished)

	_, err := svc(repo).SendEnquiry(context.Background(), ownerActor(20), 1, CreateEnquiryRequest{Message: "   "})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty message, got %v", err)
	}
}

// ─── ReplyToEnquiry ──────────────────────────────────────────────────────────

func TestReplyToEnquiry_AdNurserySuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad nursery
	repo.seedNursery(2, true, 20) // enquiring nursery
	repo.seedEnquiry(1, 1, 1, 2, EnquiryNew)

	_, err := svc(repo).ReplyToEnquiry(context.Background(), ownerActor(10), 1, ReplyEnquiryRequest{Body: "Sure!"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReplyToEnquiry_ClosedEnquiry(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.seedEnquiry(1, 1, 1, 2, EnquiryClosed)

	_, err := svc(repo).ReplyToEnquiry(context.Background(), ownerActor(10), 1, ReplyEnquiryRequest{Body: "Late reply"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for closed enquiry, got %v", err)
	}
}

func TestReplyToEnquiry_UnrelatedNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad nursery
	repo.seedNursery(2, true, 20) // enquiring nursery
	repo.seedNursery(3, true, 30) // unrelated
	repo.seedEnquiry(1, 1, 1, 2, EnquiryNew)

	_, err := svc(repo).ReplyToEnquiry(context.Background(), ownerActor(30), 1, ReplyEnquiryRequest{Body: "Interloper"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for unrelated nursery, got %v", err)
	}
}

// ─── CancelEnquiry ───────────────────────────────────────────────────────────

func TestCancelEnquiry_EnquiringNurserySuccess(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad nursery
	repo.seedNursery(2, true, 20) // enquiring nursery
	repo.seedEnquiry(1, 1, 1, 2, EnquiryNew)

	_, err := svc(repo).CancelEnquiry(context.Background(), ownerActor(20), 1)
	if err != nil {
		t.Fatalf("enquiring nursery should be able to cancel: %v", err)
	}
}

func TestCancelEnquiry_AdNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10) // ad nursery
	repo.seedNursery(2, true, 20) // enquiring nursery
	repo.seedEnquiry(1, 1, 1, 2, EnquiryNew)

	// Ad nursery (10) tries to cancel — only enquiring nursery (20) can cancel
	_, err := svc(repo).CancelEnquiry(context.Background(), ownerActor(10), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for ad nursery cancelling enquiry, got %v", err)
	}
}

// ─── BrowseAds / MyAds ───────────────────────────────────────────────────────

func TestBrowseAds_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).BrowseAds(context.Background(), buyerActor(1), AdsQuery{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer browsing, got %v", err)
	}
}

func TestBrowseAds_OwnerCanBrowse(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.seedAd(1, 1, StatusPublished)
	repo.seedAd(2, 2, StatusPublished)

	ads, total, err := svc(repo).BrowseAds(context.Background(), ownerActor(10), AdsQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("want 2 published ads, got %d", total)
	}
	_ = ads
}

func TestMyAds_AdminForbidden(t *testing.T) {
	// Admin has no nursery → actorNurseryID fails → ErrForbidden
	_, _, err := svc(newMock()).MyAds(context.Background(), adminActor(1), AdsQuery{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for admin MyAds, got %v", err)
	}
}

func TestMyAds_OwnerSeesOwnAds(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.seedAd(1, 1, StatusDraft)
	repo.seedAd(2, 2, StatusPublished)

	ads, total, err := svc(repo).MyAds(context.Background(), ownerActor(10), AdsQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || ads[0].NurseryID != 1 {
		t.Errorf("want 1 ad for nursery 1, got %d", total)
	}
}
