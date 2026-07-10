package sourcing

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	members  map[int64]*Member       // nurseryID → member
	isMember map[string]bool         // "nurseryID:userID"
	posts    map[int64]*SourcingPost // postID → post
	nextID   int64
}

func newMock() *mockRepo {
	return &mockRepo{
		members:  make(map[int64]*Member),
		isMember: make(map[string]bool),
		posts:    make(map[int64]*SourcingPost),
		nextID:   100,
	}
}

func mkKey(a, b int64) string { return fmt.Sprintf("%d:%d", a, b) }

func (m *mockRepo) next() int64 { m.nextID++; return m.nextID }

func (m *mockRepo) seedMember(nurseryID, userID int64) {
	m.isMember[mkKey(nurseryID, userID)] = true
}

func (m *mockRepo) seedPost(id, nurseryID int64, status string) *SourcingPost {
	p := &SourcingPost{ID: id, NurseryID: nurseryID, Status: status, PostType: "NEED", PlantName: "Rose"}
	m.posts[id] = p
	return p
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

func (m *mockRepo) ListFeaturedPlants(_ context.Context, nurseryID int64) ([]FeaturedPlant, error) {
	return nil, nil
}

func (m *mockRepo) AddFeaturedPlant(_ context.Context, nurseryID, userID int64, req CreateFeaturedPlantRequest) (*FeaturedPlant, error) {
	fp := &FeaturedPlant{ID: m.next(), NurseryID: nurseryID, PlantID: req.PlantID}
	return fp, nil
}

func (m *mockRepo) UpdateFeaturedPlant(_ context.Context, featuredID int64, req UpdateFeaturedPlantRequest) (*FeaturedPlant, error) {
	fp := &FeaturedPlant{ID: featuredID}
	return fp, nil
}

func (m *mockRepo) DeleteFeaturedPlant(_ context.Context, featuredID int64) error {
	return nil
}

func (m *mockRepo) ListPosts(_ context.Context, q ListPostsQuery) ([]SourcingPost, int64, error) {
	var result []SourcingPost
	for _, p := range m.posts {
		result = append(result, *p)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) GetPost(_ context.Context, postID int64) (*SourcingPost, error) {
	p, ok := m.posts[postID]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockRepo) CreatePost(_ context.Context, userID int64, req CreatePostRequest) (*SourcingPost, error) {
	p := &SourcingPost{ID: m.next(), NurseryID: req.NurseryID, PostType: req.PostType, PlantName: req.PlantName, Status: "OPEN", Urgency: req.Urgency}
	m.posts[p.ID] = p
	return p, nil
}

func (m *mockRepo) UpdatePost(_ context.Context, postID int64, req UpdatePostRequest) (*SourcingPost, error) {
	p, ok := m.posts[postID]
	if !ok {
		return nil, ErrNotFound
	}
	p.Status = req.Status
	p.Urgency = req.Urgency
	return p, nil
}

func (m *mockRepo) DeletePost(_ context.Context, postID int64) error {
	delete(m.posts, postID)
	return nil
}

func (m *mockRepo) ListResponses(_ context.Context, postID int64) ([]PostResponse, error) {
	return nil, nil
}

func (m *mockRepo) CreateResponse(_ context.Context, postID, userID int64, req CreateResponseRequest) (*PostResponse, error) {
	r := &PostResponse{ID: m.next(), PostID: postID, ResponderNurseryID: req.ResponderNurseryID, Status: "PENDING"}
	return r, nil
}

func (m *mockRepo) UpdateResponse(_ context.Context, responseID int64, req UpdateResponseRequest) (*PostResponse, error) {
	r := &PostResponse{ID: responseID, Status: req.Status}
	return r, nil
}

func (m *mockRepo) IsNurseryMember(_ context.Context, nurseryID, userID int64) (bool, error) {
	return m.isMember[mkKey(nurseryID, userID)], nil
}

func (m *mockRepo) IsNurseryOwner(_ context.Context, nurseryID, userID int64) (bool, error) {
	return false, nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}} }
func managerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"MANAGER"}} }
func buyerActor(id int64) ActorContext   { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func driverActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── canSource gate ───────────────────────────────────────────────────────────

func TestListPosts_BuyerForbidden(t *testing.T) {
	_, _, err := svc(newMock()).ListPosts(context.Background(), buyerActor(1), ListPostsQuery{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestListPosts_DriverForbidden(t *testing.T) {
	_, _, err := svc(newMock()).ListPosts(context.Background(), driverActor(1), ListPostsQuery{})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for driver, got %v", err)
	}
}

func TestListPosts_OwnerSuccess(t *testing.T) {
	_, _, err := svc(newMock()).ListPosts(context.Background(), ownerActor(10), ListPostsQuery{})
	if err != nil {
		t.Fatalf("owner should access sourcing: %v", err)
	}
}

func TestListPosts_ManagerSuccess(t *testing.T) {
	_, _, err := svc(newMock()).ListPosts(context.Background(), managerActor(10), ListPostsQuery{})
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

func TestListFeaturedPlants_ManagerSuccess(t *testing.T) {
	_, err := svc(newMock()).ListFeaturedPlants(context.Background(), managerActor(10), 1)
	if err != nil {
		t.Fatalf("manager should list featured plants: %v", err)
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

// ─── AddFeaturedPlant ─────────────────────────────────────────────────────────

func TestAddFeaturedPlant_MemberSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	fp, err := svc(repo).AddFeaturedPlant(context.Background(), ownerActor(10), 1, CreateFeaturedPlantRequest{PlantID: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.PlantID != 5 {
		t.Errorf("want PlantID 5, got %d", fp.PlantID)
	}
}

func TestAddFeaturedPlant_ZeroPlantIDInvalid(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	_, err := svc(repo).AddFeaturedPlant(context.Background(), ownerActor(10), 1, CreateFeaturedPlantRequest{PlantID: 0})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for PlantID=0, got %v", err)
	}
}

func TestAddFeaturedPlant_NonMemberForbidden(t *testing.T) {
	_, err := svc(newMock()).AddFeaturedPlant(context.Background(), ownerActor(10), 1, CreateFeaturedPlantRequest{PlantID: 5})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member, got %v", err)
	}
}

// ─── CreatePost ──────────────────────────────────────────────────────────────

func TestCreatePost_OwnerMemberSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)

	p, err := svc(repo).CreatePost(context.Background(), ownerActor(10), CreatePostRequest{
		NurseryID: 1,
		PostType:  "NEED",
		PlantName: "Rose",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PlantName != "Rose" {
		t.Errorf("want Rose, got %s", p.PlantName)
	}
}

func TestCreatePost_AdminSuccess(t *testing.T) {
	_, err := svc(newMock()).CreatePost(context.Background(), adminActor(1), CreatePostRequest{
		NurseryID: 1,
		PostType:  "AVAILABLE",
		PlantName: "Neem",
	})
	if err != nil {
		t.Fatalf("admin should create post: %v", err)
	}
}

func TestCreatePost_NonMemberForbidden(t *testing.T) {
	_, err := svc(newMock()).CreatePost(context.Background(), ownerActor(10), CreatePostRequest{
		NurseryID: 1,
		PostType:  "NEED",
		PlantName: "Rose",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member, got %v", err)
	}
}

func TestCreatePost_EmptyPlantNameInvalid(t *testing.T) {
	_, err := svc(newMock()).CreatePost(context.Background(), adminActor(1), CreatePostRequest{
		NurseryID: 1,
		PostType:  "NEED",
		PlantName: "   ",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty plant name, got %v", err)
	}
}

func TestCreatePost_BadPostTypeInvalid(t *testing.T) {
	_, err := svc(newMock()).CreatePost(context.Background(), adminActor(1), CreatePostRequest{
		NurseryID: 1,
		PostType:  "WANTED",
		PlantName: "Rose",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad PostType, got %v", err)
	}
}

func TestCreatePost_UrgencyNormalNormalized(t *testing.T) {
	// "NORMAL" urgency should be normalized to "FLEXIBLE"
	p, err := svc(newMock()).CreatePost(context.Background(), adminActor(1), CreatePostRequest{
		NurseryID: 1,
		PostType:  "NEED",
		PlantName: "Rose",
		Urgency:   "NORMAL",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Urgency != "FLEXIBLE" {
		t.Errorf("want FLEXIBLE after normalizing NORMAL, got %s", p.Urgency)
	}
}

// ─── UpdatePost ──────────────────────────────────────────────────────────────

func TestUpdatePost_OwnerSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedPost(1, 1, "OPEN")

	p, err := svc(repo).UpdatePost(context.Background(), ownerActor(10), 1, UpdatePostRequest{
		PlantName: "Jasmine",
		Status:    "CLOSED",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status != "CLOSED" {
		t.Errorf("want CLOSED, got %s", p.Status)
	}
}

func TestUpdatePost_BadStatusInvalid(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedPost(1, 1, "OPEN")

	_, err := svc(repo).UpdatePost(context.Background(), ownerActor(10), 1, UpdatePostRequest{
		Status: "DELETED",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for bad status, got %v", err)
	}
}

func TestUpdatePost_UrgencyHighNormalized(t *testing.T) {
	repo := newMock()
	repo.seedPost(1, 1, "OPEN")

	p, err := svc(repo).UpdatePost(context.Background(), adminActor(1), 1, UpdatePostRequest{
		Status:  "OPEN",
		Urgency: "HIGH",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Urgency != "URGENT" {
		t.Errorf("want URGENT after normalizing HIGH, got %s", p.Urgency)
	}
}

func TestUpdatePost_NonMemberForbidden(t *testing.T) {
	repo := newMock()
	repo.seedPost(1, 1, "OPEN")

	_, err := svc(repo).UpdatePost(context.Background(), ownerActor(20), 1, UpdatePostRequest{Status: "CLOSED"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member, got %v", err)
	}
}

// ─── DeletePost ───────────────────────────────────────────────────────────────

func TestDeletePost_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedPost(1, 1, "OPEN")

	err := svc(repo).DeletePost(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.posts[1]; ok {
		t.Error("post should have been deleted")
	}
}

func TestDeletePost_NonMemberForbidden(t *testing.T) {
	repo := newMock()
	repo.seedPost(1, 1, "OPEN")

	err := svc(repo).DeletePost(context.Background(), ownerActor(20), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member, got %v", err)
	}
}

// ─── CreateResponse ───────────────────────────────────────────────────────────

func TestCreateResponse_OtherNurserySuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(2, 20) // responder is member of nursery 2
	repo.seedPost(1, 1, "OPEN")

	resp, err := svc(repo).CreateResponse(context.Background(), ownerActor(20), 1, CreateResponseRequest{
		ResponderNurseryID: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "PENDING" {
		t.Errorf("want PENDING, got %s", resp.Status)
	}
}

func TestCreateResponse_SameNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedPost(1, 1, "OPEN") // post belongs to nursery 1

	// same nursery tries to respond — should be ErrInvalidInput
	_, err := svc(repo).CreateResponse(context.Background(), ownerActor(10), 1, CreateResponseRequest{
		ResponderNurseryID: 1,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for self-response, got %v", err)
	}
}

func TestCreateResponse_NonMemberForbidden(t *testing.T) {
	repo := newMock()
	repo.seedPost(1, 1, "OPEN")

	_, err := svc(repo).CreateResponse(context.Background(), ownerActor(20), 1, CreateResponseRequest{
		ResponderNurseryID: 2,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-member responder, got %v", err)
	}
}

// ─── UpdateResponse ───────────────────────────────────────────────────────────

func TestUpdateResponse_AcceptSuccess(t *testing.T) {
	repo := newMock()
	repo.seedMember(1, 10)
	repo.seedPost(1, 1, "OPEN") // post owned by nursery 1

	resp, err := svc(repo).UpdateResponse(context.Background(), ownerActor(10), 1, 50, UpdateResponseRequest{Status: "ACCEPTED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "ACCEPTED" {
		t.Errorf("want ACCEPTED, got %s", resp.Status)
	}
}

func TestUpdateResponse_BadStatusInvalid(t *testing.T) {
	repo := newMock()
	repo.seedPost(1, 1, "OPEN")

	_, err := svc(repo).UpdateResponse(context.Background(), adminActor(1), 1, 50, UpdateResponseRequest{Status: "PENDING"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for status PENDING (not ACCEPTED/DECLINED), got %v", err)
	}
}

func TestUpdateResponse_NonOwnerNurseryForbidden(t *testing.T) {
	repo := newMock()
	repo.seedPost(1, 1, "OPEN") // post owned by nursery 1
	// actor is member of nursery 2, not nursery 1

	_, err := svc(repo).UpdateResponse(context.Background(), ownerActor(20), 1, 50, UpdateResponseRequest{Status: "ACCEPTED"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for non-post-owner nursery, got %v", err)
	}
}
