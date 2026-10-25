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
	lastOfferBody string
	blockedUntil  map[int64]*time.Time
	noCoords      map[int64]bool
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
		blockedUntil:  make(map[int64]*time.Time),
		noCoords:      make(map[int64]bool),
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
	lat, lon := 17.44, 78.35
	ad := Ad{ID: id, NurseryID: nurseryID, Status: status, Title: "Test Ad", PlantName: "Rose",
		PickupLatitude: &lat, PickupLongitude: &lon}
	m.ads[id] = ad
	return ad
}

// seedEnquiry also seeds its ad as PUBLISHED unless the test already seeded it.
func (m *mockRepo) seedEnquiry(id, adID, adNurseryID, enquiringNurseryID int64, status string) Enquiry {
	if _, ok := m.ads[adID]; !ok {
		m.seedAd(adID, adNurseryID, StatusPublished)
	}
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
	if !ok || ad.Status == StatusDeleted {
		return Ad{}, ErrNotFound
	}
	return ad, nil
}

func (m *mockRepo) IsCatalogSize(_ context.Context, name string) (bool, error) {
	switch name {
	case "Seed", "Sapling", "Small", "Medium", "Large", "Extra Large":
		return true, nil
	}
	return false, nil
}

func (m *mockRepo) SoftDeleteAd(_ context.Context, id, _ int64) (bool, error) {
	ad, ok := m.ads[id]
	if !ok {
		return false, nil
	}
	for _, e := range m.enquiries {
		if e.AdID == id {
			return false, nil
		}
	}
	if ad.Status != StatusDraft && ad.Status != StatusPublished && ad.Status != StatusPaused {
		return false, nil
	}
	ad.Status = StatusDeleted
	m.ads[id] = ad
	return true, nil
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

func (m *mockRepo) ModerateAd(_ context.Context, id int64, status string, note *string) error {
	ad := m.ads[id]
	ad.Status, ad.ModerationNote = status, note
	m.ads[id] = ad
	return nil
}

func (m *mockRepo) SellerProfile(_ context.Context, nurseryID int64) (SellerProfile, error) {
	return SellerProfile{NurseryID: nurseryID}, nil
}

func (m *mockRepo) MarkChatRead(_ context.Context, _, _ int64) error { return nil }

func (m *mockRepo) MarkChatsDelivered(_ context.Context, _ int64) error { return nil }

func (m *mockRepo) ChatSummaries(_ context.Context, _ []Enquiry, _ int64) error { return nil }

func (m *mockRepo) UnreadTotal(_ context.Context, _ int64) (int, error) { return 0, nil }

// NurseryPickup: every mock nursery sits in Hyderabad unless listed in noCoords.
func (m *mockRepo) SetAdPickupLocation(_ context.Context, adID int64, lat, lon float64, _ string) error {
	ad := m.ads[adID]
	ad.PickupLatitude, ad.PickupLongitude = &lat, &lon
	m.ads[adID] = ad
	return nil
}

func (m *mockRepo) NurseryPickup(_ context.Context, nurseryID int64) (*string, *float64, *float64, error) {
	if m.noCoords[nurseryID] {
		return nil, nil, nil, nil
	}
	lat, lon := 17.44, 78.35
	return nil, &lat, &lon, nil
}

func (m *mockRepo) PostingBlockedUntil(_ context.Context, nurseryID int64) (*time.Time, error) {
	return m.blockedUntil[nurseryID], nil
}

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
	ad := m.ads[e.AdID]
	e.AdStatus, e.AdQuantity, e.AdPriceUnit = ad.Status, ad.Quantity, ad.PriceUnit
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

// AddOffer and RespondToOffer only record the call: the offer rules that
// live in SQL are covered by the API journeys against a real database.
func (m *mockRepo) AddOffer(_ context.Context, enquiryID, userID, nurseryID int64, req MakeOfferRequest, body string) (Message, error) {
	m.lastOfferBody = body
	return Message{ID: 2, EnquiryID: enquiryID, SentByUserID: userID, SentByNurseryID: nurseryID, Type: MessageOffer, Body: body}, nil
}

func (m *mockRepo) RespondToOffer(_ context.Context, enquiryID, _, _, _ int64, accept bool, dealBody func(float64, *int) string) error {
	if accept {
		e := m.enquiries[enquiryID]
		e.Status = EnquiryDealAgreed
		m.enquiries[enquiryID] = e
	}
	return nil
}

func (m *mockRepo) GetMessages(_ context.Context, _ int64) ([]Message, error) { return nil, nil }

func (m *mockRepo) EnquiryIDFor(_ context.Context, adID, nurseryID int64) (int64, error) {
	for id, e := range m.enquiries {
		if e.AdID == adID && e.EnquiringNurseryID == nurseryID {
			return id, nil
		}
	}
	if m.enquiryExists[mk(adID, nurseryID)] {
		return 999, nil // seeded only as "exists"; the ID does not matter
	}
	return 0, nil
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

func TestDeleteAd(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		status  string
		chatted bool
		actor   int64
		wantErr error
	}{
		{"draft", StatusDraft, false, 10, nil},
		{"live, no chats", StatusPublished, false, 10, nil},
		{"paused, no chats", StatusPaused, false, 10, nil},
		{"live with a chat", StatusPublished, true, 10, ErrConflict},
		{"archived", StatusArchived, false, 10, ErrConflict},
		{"expired", StatusExpired, false, 10, ErrConflict},
		{"removed by admin", StatusRemoved, false, 10, ErrConflict},
		{"another nursery's ad", StatusDraft, false, 20, ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMock()
			repo.seedNursery(1, true, 10)
			repo.seedNursery(2, true, 20)
			ad := repo.seedAd(1, 1, tc.status)
			if tc.chatted {
				repo.seedEnquiry(5, 1, 1, 2, EnquiryNew)
				ad.EnquiryCount = 1
				repo.ads[1] = ad
			}
			err := svc(repo).DeleteAd(ctx, ownerActor(tc.actor), 1)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
			_, getErr := repo.GetAd(ctx, 1)
			if deleted := errors.Is(getErr, ErrNotFound); deleted != (tc.wantErr == nil) {
				t.Errorf("deleted = %v, want %v", deleted, tc.wantErr == nil)
			}
		})
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
	if !errors.Is(err, ErrConflict) {
		t.Errorf("want ErrConflict for closed enquiry, got %v", err)
	}
}

func TestChatIsReadOnlyOnceTheAdIsNotLive(t *testing.T) {
	for _, status := range []string{StatusPaused, StatusExpired, StatusArchived} {
		repo := newMock()
		repo.seedNursery(1, true, 10)
		repo.seedNursery(2, true, 20)
		repo.seedAd(1, 1, status)
		repo.seedEnquiry(1, 1, 1, 2, EnquiryInProgress)
		s := svc(repo)

		if _, err := s.ReplyToEnquiry(context.Background(), ownerActor(20), 1, ReplyEnquiryRequest{Body: "still there?"}); !errors.Is(err, ErrConflict) {
			t.Errorf("%s ad: reply want ErrConflict, got %v", status, err)
		}
		if _, err := s.MakeOffer(context.Background(), ownerActor(20), 1, MakeOfferRequest{PricePerUnit: 50}); !errors.Is(err, ErrConflict) {
			t.Errorf("%s ad: offer want ErrConflict, got %v", status, err)
		}
		e, err := s.GetEnquiry(context.Background(), ownerActor(20), 1)
		if err != nil {
			t.Fatal(err)
		}
		if e.Capabilities.CanReply || e.Capabilities.CanOffer || e.Capabilities.ReadOnlyReason == "" {
			t.Errorf("%s ad: want a read-only chat with a reason, got %+v", status, e.Capabilities)
		}
	}
}

func TestMakeOffer(t *testing.T) {
	setup := func() (*mockRepo, *Service) {
		repo := newMock()
		repo.seedNursery(1, true, 10)
		repo.seedNursery(2, true, 20)
		ad := repo.seedAd(1, 1, StatusPublished)
		qty, unit := 100, "PER_PLANT"
		ad.Quantity, ad.PriceUnit = &qty, &unit
		repo.ads[1] = ad
		repo.seedEnquiry(1, 1, 1, 2, EnquiryInProgress)
		return repo, svc(repo)
	}
	ctx := context.Background()
	fifty, tooMany := 50, 101

	repo, s := setup()
	msg, err := s.MakeOffer(ctx, ownerActor(20), 1, MakeOfferRequest{PricePerUnit: 120, Quantity: &fifty})
	if err != nil || !msg.IsMine {
		t.Fatalf("buyer offer: %v %+v", err, msg)
	}
	if repo.lastOfferBody != "Offered ₹120 per plant × 50" {
		t.Errorf("offer text = %q", repo.lastOfferBody)
	}
	if _, err := s.MakeOffer(ctx, ownerActor(10), 1, MakeOfferRequest{PricePerUnit: 140}); err != nil {
		t.Errorf("the seller can counter: %v", err)
	}
	for name, req := range map[string]MakeOfferRequest{
		"zero price":    {PricePerUnit: 0},
		"negative":      {PricePerUnit: -5},
		"over the ad's": {PricePerUnit: 10, Quantity: &tooMany},
	} {
		if _, err := s.MakeOffer(ctx, ownerActor(20), 1, req); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}
	if _, err := s.MakeOffer(ctx, ownerActor(30), 1, MakeOfferRequest{PricePerUnit: 10}); !errors.Is(err, ErrForbidden) {
		t.Errorf("an outsider: want ErrForbidden, got %v", err)
	}

	_, s = setup()
	if _, err := s.AcceptOffer(ctx, ownerActor(10), 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MakeOffer(ctx, ownerActor(20), 1, MakeOfferRequest{PricePerUnit: 99}); !errors.Is(err, ErrConflict) {
		t.Errorf("no more offers after a deal: want ErrConflict, got %v", err)
	}
	if _, err := s.ReplyToEnquiry(ctx, ownerActor(20), 1, ReplyEnquiryRequest{Body: "When can I pick up?"}); err != nil {
		t.Errorf("chat stays open after a deal to arrange pickup: %v", err)
	}
}

func TestEndEnquiryRules(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.seedEnquiry(1, 1, 1, 2, EnquiryInProgress)
	s := svc(repo)
	ctx := context.Background()

	if _, err := s.CloseEnquiry(ctx, ownerActor(20), 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("the buyer cannot close: got %v", err)
	}
	if _, err := s.CloseEnquiry(ctx, ownerActor(10), 1); err != nil {
		t.Fatalf("the seller closes: %v", err)
	}
	if _, err := s.CancelEnquiry(ctx, ownerActor(20), 1); !errors.Is(err, ErrConflict) {
		t.Errorf("a closed chat cannot be cancelled: got %v", err)
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

// ─── Moderation ──────────────────────────────────────────────────────────────

func TestSeriousReportHidesTheAdAtOnce(t *testing.T) {
	ctx := context.Background()
	for reason, wantHidden := range map[string]bool{"EXPLICIT_CONTENT": true, "PROHIBITED": true, "SPAM": false, "SCAM": false} {
		repo := newMock()
		repo.seedNursery(1, true, 10)
		repo.seedNursery(2, true, 20)
		repo.seedAd(1, 1, StatusPublished)
		if err := svc(repo).ReportAd(ctx, ownerActor(20), 1, ReportAdRequest{Reason: reason}); err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		hidden := repo.ads[1].Status == StatusSuspended
		if hidden != wantHidden {
			t.Errorf("%s: hidden = %v, want %v", reason, hidden, wantHidden)
		}
		if hidden && (repo.ads[1].ModerationNote == nil || *repo.ads[1].ModerationNote == "") {
			t.Errorf("%s: the seller needs a note saying why", reason)
		}
	}
}

func TestSuspendedAdIsFrozenForTheSeller(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedAd(1, 1, StatusSuspended)
	s := svc(repo)
	ctx := context.Background()
	title := "Back again"
	if _, err := s.UpdateAd(ctx, ownerActor(10), 1, UpdateAdRequest{Title: &title}); !errors.Is(err, ErrConflict) {
		t.Errorf("edit under review: want ErrConflict, got %v", err)
	}
	for name, fn := range map[string]func(context.Context, ActorContext, int64) (Ad, error){
		"publish": s.PublishAd, "resume": s.ResumeAd, "archive": s.ArchiveAd,
	} {
		if _, err := fn(ctx, ownerActor(10), 1); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s under review: want ErrInvalidInput, got %v", name, err)
		}
	}
	ad, err := s.GetAd(ctx, ownerActor(10), 1)
	if err != nil {
		t.Fatal(err)
	}
	if ad.Capabilities.CanEdit || ad.Capabilities.CanArchive {
		t.Errorf("no edit/archive buttons under review: %+v", ad.Capabilities)
	}
}

func TestBlockedNurseryCannotPost(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	until := time.Now().Add(30 * 24 * time.Hour)
	repo.blockedUntil[1] = &until
	repo.seedAd(1, 1, StatusDraft)
	repo.seedAd(2, 1, StatusPaused)
	s := svc(repo)
	ctx := context.Background()

	if _, err := s.CreateAd(ctx, ownerActor(10), CreateAdRequest{Title: "x", PlantName: "Rose"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("create while blocked: want ErrForbidden, got %v", err)
	}
	if _, err := s.PublishAd(ctx, ownerActor(10), 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("publish while blocked: want ErrForbidden, got %v", err)
	}
	if _, err := s.ResumeAd(ctx, ownerActor(10), 2); !errors.Is(err, ErrForbidden) {
		t.Errorf("resume while blocked: want ErrForbidden, got %v", err)
	}
}

func TestBlockDays(t *testing.T) {
	for strikes, want := range map[int]int{0: 0, 1: 0, 2: 30, 3: 90, 7: 90} {
		if got := blockDays(strikes); got != want {
			t.Errorf("blockDays(%d) = %d, want %d", strikes, got, want)
		}
	}
}

func TestValidateAdType(t *testing.T) {
	plant, price, unit := int64(7), 120.0, "PER_PLANT"
	future := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	cases := []struct {
		name string
		req  CreateAdRequest
		ok   bool
	}{
		{"default is SELL", CreateAdRequest{}, true},
		{"want with a catalog plant", CreateAdRequest{AdType: "want", PlantID: &plant, NeededBy: &future}, true},
		{"want without a plant", CreateAdRequest{AdType: "WANT"}, false},
		{"want with a price", CreateAdRequest{AdType: "WANT", PlantID: &plant, PricePerUnit: &price}, false},
		{"want with a price unit", CreateAdRequest{AdType: "WANT", PlantID: &plant, PriceUnit: &unit}, false},
		{"want with photos", CreateAdRequest{AdType: "WANT", PlantID: &plant, Photos: []string{"x"}}, false},
		{"want needed in the past", CreateAdRequest{AdType: "WANT", PlantID: &plant, NeededBy: &past}, false},
		{"sell with needed_by", CreateAdRequest{AdType: "SELL", NeededBy: &future}, false},
		{"unknown type", CreateAdRequest{AdType: "SWAP"}, false},
	}
	for _, c := range cases {
		req := c.req
		err := validateAdType(&req)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
		if c.ok && req.AdType != AdTypeSell && req.AdType != AdTypeWant {
			t.Errorf("%s: type not normalised: %q", c.name, req.AdType)
		}
	}
}

func TestValidateAccess(t *testing.T) {
	yes, no := true, false
	spot, blank := "Gate on NH-44", "  "
	cases := []struct {
		name        string
		road, truck *bool
		pickup      *string
		wantErr     bool
	}{
		{"not stated", nil, nil, nil, false},
		{"road and truck", &yes, &yes, nil, false},
		{"road, no truck (narrow road)", &yes, &no, nil, false},
		{"no road, pickup point given", &no, &no, &spot, false},
		{"no road, no pickup point", &no, &no, nil, true},
		{"no road, blank pickup point", &no, nil, &blank, true},
		{"truck without a road", &no, &yes, &spot, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAccess(tc.road, tc.truck, tc.pickup)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestNearbyArea(t *testing.T) {
	ctx := context.Background()
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.seedNursery(2, true, 20)
	repo.noCoords[2] = true
	f := func(v float64) *float64 { return &v }

	q := AdsQuery{}
	if err := svc(repo).nearbyArea(ctx, ownerActor(10), &q); err != nil {
		t.Fatal(err)
	}
	if *q.NearLat != 17.44 || *q.RadiusKM != DefaultRadiusKM {
		t.Errorf("no position: want the nursery's address and %v km, got %v, %v", DefaultRadiusKM, *q.NearLat, *q.RadiusKM)
	}

	q = AdsQuery{NearLat: f(28.6), NearLon: f(77.2), RadiusKM: f(500)}
	_ = svc(repo).nearbyArea(ctx, ownerActor(10), &q)
	if *q.NearLat != 28.6 || *q.RadiusKM != MaxRadiusKM {
		t.Errorf("phone position kept and radius capped at %v, got %v, %v", MaxRadiusKM, *q.NearLat, *q.RadiusKM)
	}

	if err := svc(repo).nearbyArea(ctx, ownerActor(20), &AdsQuery{}); !errors.Is(err, ErrLocationRequired) {
		t.Errorf("no phone position and no nursery coordinates: want ErrLocationRequired, got %v", err)
	}
}

func TestPublishAd_NeedsLocation(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	repo.noCoords[1] = true // neither the ad nor the nursery has a location
	ad := repo.seedAd(1, 1, StatusDraft)
	ad.PickupLatitude, ad.PickupLongitude = nil, nil
	repo.ads[1] = ad
	if _, err := svc(repo).PublishAd(context.Background(), ownerActor(10), 1); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for an ad with no location, got %v", err)
	}
}

// A draft saved without a location publishes once the nursery has one.
func TestPublishAd_FallsBackToNurseryLocation(t *testing.T) {
	repo := newMock()
	repo.seedNursery(1, true, 10)
	ad := repo.seedAd(1, 1, StatusDraft)
	ad.PickupLatitude, ad.PickupLongitude = nil, nil
	repo.ads[1] = ad
	repo.members[mk(1, 10)] = true

	if _, err := svc(repo).PublishAd(context.Background(), ownerActor(10), 1); err != nil {
		t.Fatalf("publish should use the nursery location: %v", err)
	}
	if repo.ads[1].PickupLatitude == nil {
		t.Error("ad should now carry the nursery coordinates")
	}

}

func TestMinOffer_SeventyPercentForBuyersOnly(t *testing.T) {
	price := 120.0
	sell := Enquiry{AdType: AdTypeSell, AdPricePerUnit: &price, AdNurseryID: 1, EnquiringNurseryID: 2}
	if got := minOffer(sell, 2); got != 84 {
		t.Fatalf("buyer on a ₹120 ad: want ₹84 minimum, got %v", got)
	}
	if got := minOffer(sell, 1); got != 0 {
		t.Fatalf("the seller's own counter is not limited, got %v", got)
	}
	want := sell
	want.AdType = "WANT"
	if got := minOffer(want, 2); got != 0 {
		t.Fatalf("wanted ads have no asking price to protect, got %v", got)
	}
	noPrice := sell
	noPrice.AdPricePerUnit = nil
	if got := minOffer(noPrice, 2); got != 0 {
		t.Fatalf("price on request: no limit, got %v", got)
	}
	odd := 99.0 // 69.3 rounds up to the paisa
	sell.AdPricePerUnit = &odd
	if got := minOffer(sell, 2); got != 69.3 {
		t.Fatalf("₹99 ad: want ₹69.30 minimum, got %v", got)
	}
}
