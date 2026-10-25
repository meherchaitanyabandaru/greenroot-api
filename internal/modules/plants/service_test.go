package plants

import (
	"context"
	"errors"
	"testing"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	plants     map[int64]*Plant
	categories map[int64]Category
	sizes      []PlantSize
	images     map[int64]*Image
	careGuides map[int64]*CareGuide

	botanicalQueries []string
	botanicalNames   []BotanicalName // overrides LookupBotanical's default names
	lastBrowse       ListBotanicalRequest

	nextPlantID    int64
	nextCategoryID int64
	nextImageID    int64
}

func newMock() *mockRepo {
	return &mockRepo{
		plants:         make(map[int64]*Plant),
		categories:     make(map[int64]Category),
		images:         make(map[int64]*Image),
		careGuides:     make(map[int64]*CareGuide),
		nextPlantID:    100,
		nextCategoryID: 200,
		nextImageID:    300,
	}
}

func (m *mockRepo) seedPlant(id int64, sciName string) *Plant {
	p := &Plant{ID: id, ScientificName: sciName, IsActive: true}
	m.plants[id] = p
	return p
}

func (m *mockRepo) List(_ context.Context, req ListPlantsRequest) ([]Plant, int64, error) {
	result := make([]Plant, 0, len(m.plants))
	for _, p := range m.plants {
		result = append(result, *p)
	}
	return result, int64(len(result)), nil
}

func (m *mockRepo) FindByID(_ context.Context, id int64) (*Plant, error) {
	p, ok := m.plants[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockRepo) Create(_ context.Context, input CreatePlantRequest) (*Plant, error) {
	m.nextPlantID++
	p := &Plant{ID: m.nextPlantID, ScientificName: input.ScientificName, IsActive: true}
	m.plants[m.nextPlantID] = p
	return p, nil
}

func (m *mockRepo) Update(_ context.Context, id int64, input UpdatePlantRequest) (*Plant, error) {
	p, ok := m.plants[id]
	if !ok {
		return nil, ErrNotFound
	}
	p.ScientificName = input.ScientificName
	return p, nil
}

func (m *mockRepo) Delete(_ context.Context, id int64) error {
	if _, ok := m.plants[id]; !ok {
		return ErrNotFound
	}
	delete(m.plants, id)
	return nil
}

func (m *mockRepo) ListSizes(_ context.Context) ([]PlantSize, error) { return m.sizes, nil }

func (m *mockRepo) ListCategories(_ context.Context) ([]Category, error) {
	result := make([]Category, 0, len(m.categories))
	for _, c := range m.categories {
		result = append(result, c)
	}
	return result, nil
}

func (m *mockRepo) CreateCategory(_ context.Context, name string) (Category, error) {
	m.nextCategoryID++
	c := Category{ID: m.nextCategoryID, Name: name}
	m.categories[m.nextCategoryID] = c
	return c, nil
}

func (m *mockRepo) UpdateCategory(_ context.Context, id int64, input UpdateCategoryRequest) (Category, error) {
	c, ok := m.categories[id]
	if !ok {
		return Category{}, ErrNotFound
	}
	if input.Name != nil {
		c.Name = *input.Name
	}
	m.categories[id] = c
	return c, nil
}

func (m *mockRepo) DeleteCategory(_ context.Context, id int64) error {
	if _, ok := m.categories[id]; !ok {
		return ErrNotFound
	}
	delete(m.categories, id)
	return nil
}

func (m *mockRepo) CreateImage(_ context.Context, plantID int64, input CreateImageRequest) (*Image, error) {
	m.nextImageID++
	img := &Image{ID: m.nextImageID, PlantID: plantID, ImageURL: input.ImageURL}
	m.images[m.nextImageID] = img
	return img, nil
}

func (m *mockRepo) GetCareGuide(_ context.Context, plantID int64) (*CareGuide, error) {
	g, ok := m.careGuides[plantID]
	if !ok {
		return nil, ErrNotFound
	}
	return g, nil
}

func (m *mockRepo) BotanicalNameExists(_ context.Context, wfoID string) (bool, error) {
	return wfoID == "wfo-known", nil
}

func (m *mockRepo) LookupBotanical(_ context.Context, query string, limit int) ([]BotanicalName, error) {
	m.botanicalQueries = append(m.botanicalQueries, query)
	names := []BotanicalName{{WfoID: "wfo-1", ScientificName: "Syzygium cumini"}, {WfoID: "wfo-2", ScientificName: "Syzygium jambos"}}
	if m.botanicalNames != nil {
		names = m.botanicalNames
	}
	if len(names) > limit {
		names = names[:limit]
	}
	return names, nil
}

func (m *mockRepo) ListBotanical(_ context.Context, in ListBotanicalRequest) ([]BotanicalRow, int64, error) {
	m.lastBrowse = in
	return []BotanicalRow{{BotanicalName: BotanicalName{WfoID: "wfo-1", ScientificName: "Syzygium cumini"}}}, 1657866, nil
}

func (m *mockRepo) BotanicalFamilies(context.Context) ([]BotanicalFamily, error) {
	return []BotanicalFamily{{Family: "Myrtaceae", Names: 6000}, {Family: "Rosaceae", Names: 9000}}, nil
}

func (m *mockRepo) GetBotanical(_ context.Context, wfoID string) (BotanicalDetailResponse, error) {
	return BotanicalDetailResponse{Name: BotanicalRow{BotanicalName: BotanicalName{WfoID: wfoID}}}, nil
}

func (m *mockRepo) ResolveBotanical(_ context.Context, wfoID string) (int64, bool, error) {
	if wfoID != "wfo-1" {
		return 0, false, ErrNotFound
	}
	for id, p := range m.plants {
		if p.ScientificName == "Syzygium cumini" {
			return id, false, nil
		}
	}
	m.nextPlantID++
	m.plants[m.nextPlantID] = &Plant{ID: m.nextPlantID, ScientificName: "Syzygium cumini", IsActive: true}
	return m.nextPlantID, true, nil
}

// ─── actors ──────────────────────────────────────────────────────────────────

func adminActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}
func buyerActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }

func svc(repo *mockRepo) *Service { return NewService(repo, nil) }

// ─── List / Get ───────────────────────────────────────────────────────────────

func TestList_AnyoneCanList(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")
	repo.seedPlant(2, "Neem")

	plants, _, err := svc(repo).List(context.Background(), ListPlantsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plants) != 2 {
		t.Errorf("want 2 plants, got %d", len(plants))
	}
}

func TestGet_Found(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")

	plant, err := svc(repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plant.ScientificName != "Rosa indica" {
		t.Errorf("want Rosa indica, got %s", plant.ScientificName)
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := svc(newMock()).Get(context.Background(), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Create (admin-only) ──────────────────────────────────────────────────────

func TestCreate_AdminSuccess(t *testing.T) {
	plant, err := svc(newMock()).Create(context.Background(), adminActor(1), CreatePlantRequest{
		ScientificName: "Rosa indica",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plant.ScientificName != "Rosa indica" {
		t.Errorf("want Rosa indica, got %s", plant.ScientificName)
	}
}

func TestCreate_OwnerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), ownerActor(10), CreatePlantRequest{
		ScientificName: "Rosa indica",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for nursery owner, got %v", err)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), CreatePlantRequest{
		ScientificName: "Rosa",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_EmptyScientificNameInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreatePlantRequest{
		ScientificName: "   ",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty scientific name, got %v", err)
	}
}

func TestCreate_InvalidCategoryIDInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), adminActor(1), CreatePlantRequest{
		ScientificName: "Rosa indica",
		CategoryIDs:    []int64{0}, // invalid
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for category ID 0, got %v", err)
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func TestUpdate_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")

	plant, err := svc(repo).Update(context.Background(), adminActor(1), 1, UpdatePlantRequest{
		ScientificName: "Rosa damascena",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plant.ScientificName != "Rosa damascena" {
		t.Errorf("want Rosa damascena, got %s", plant.ScientificName)
	}
}

func TestUpdate_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")

	_, err := svc(repo).Update(context.Background(), buyerActor(1), 1, UpdatePlantRequest{ScientificName: "Hack"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── Delete ──────────────────────────────────────────────────────────────────

func TestDelete_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")

	err := svc(repo).Delete(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := repo.plants[1]; ok {
		t.Error("plant should have been deleted")
	}
}

func TestDelete_NotFound(t *testing.T) {
	err := svc(newMock()).Delete(context.Background(), adminActor(1), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Category CRUD (admin-only) ───────────────────────────────────────────────

func TestCreateCategory_AdminSuccess(t *testing.T) {
	c, err := svc(newMock()).CreateCategory(context.Background(), adminActor(1), CreateCategoryRequest{Name: "Flowering"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Name != "Flowering" {
		t.Errorf("want Flowering, got %s", c.Name)
	}
}

func TestCreateCategory_OwnerForbidden(t *testing.T) {
	_, err := svc(newMock()).CreateCategory(context.Background(), ownerActor(10), CreateCategoryRequest{Name: "Hack"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

func TestCreateCategory_EmptyNameInvalid(t *testing.T) {
	_, err := svc(newMock()).CreateCategory(context.Background(), adminActor(1), CreateCategoryRequest{Name: "   "})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestDeleteCategory_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.categories[1] = Category{ID: 1, Name: "Flowering"}

	err := svc(repo).DeleteCategory(context.Background(), adminActor(1), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteCategory_BuyerForbidden(t *testing.T) {
	err := svc(newMock()).DeleteCategory(context.Background(), buyerActor(1), 1)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── CreateImage ──────────────────────────────────────────────────────────────

func TestCreateImage_AdminSuccess(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")

	img, err := svc(repo).CreateImage(context.Background(), adminActor(1), 1, CreateImageRequest{ImageURL: "https://example.com/rose.jpg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img.ImageURL != "https://example.com/rose.jpg" {
		t.Errorf("want image URL, got %s", img.ImageURL)
	}
}

func TestCreateImage_EmptyURLInvalid(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")

	_, err := svc(repo).CreateImage(context.Background(), adminActor(1), 1, CreateImageRequest{ImageURL: "   "})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for empty URL, got %v", err)
	}
}

func TestCreateImage_BuyerForbidden(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")

	_, err := svc(repo).CreateImage(context.Background(), buyerActor(1), 1, CreateImageRequest{ImageURL: "http://x.com/x.jpg"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ─── GetCareGuide ─────────────────────────────────────────────────────────────

func TestGetCareGuide_Found(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Rosa indica")
	repo.careGuides[1] = &CareGuide{PlantID: 1}

	_, err := svc(repo).GetCareGuide(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetCareGuide_NotFound(t *testing.T) {
	_, err := svc(newMock()).GetCareGuide(context.Background(), 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ─── Lookup / Resolve ────────────────────────────────────────────────────────

func TestLookup_CatalogFirstThenBotanical(t *testing.T) {
	repo := newMock()
	repo.seedPlant(1, "Syzygium aromaticum")
	results, err := svc(repo).Lookup(context.Background(), LookupRequest{Query: "  syzygium  ", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Plant == nil || results[0].Plant.ID != 1 || results[1].Name == nil || results[1].Name.WfoID != "wfo-1" {
		t.Fatalf("want catalog plant then one botanical name, got %+v", results)
	}
	if len(repo.botanicalQueries) != 1 || repo.botanicalQueries[0] != "syzygium" {
		t.Fatalf("botanical query not trimmed: %q", repo.botanicalQueries)
	}
}

func TestLookup_DuplicateWFORecordsShowOnce(t *testing.T) {
	repo := newMock()
	author := "É.Desv."
	repo.botanicalNames = []BotanicalName{
		{WfoID: "wfo-a", ScientificName: "Bromus mango", Authorship: &author},
		{WfoID: "wfo-b", ScientificName: "Bromus mango", Authorship: &author},
		{WfoID: "wfo-c", ScientificName: "Mangonia uruguaya"},
	}
	results, err := svc(repo).Lookup(context.Background(), LookupRequest{Query: "mango"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Name.WfoID != "wfo-a" || results[1].Name.WfoID != "wfo-c" {
		t.Fatalf("want Bromus mango once then Mangonia, got %+v", results)
	}
}

func TestLookup_ShortQuerySkipsBotanicalNames(t *testing.T) {
	repo := newMock()
	if _, err := svc(repo).Lookup(context.Background(), LookupRequest{Query: "sy"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.botanicalQueries) != 0 {
		t.Fatal("2-letter query must not search 1.66 M botanical names")
	}
}

func TestResolve_AnySignedInUserAddsPlantOnce(t *testing.T) {
	repo := newMock()
	s := svc(repo)
	first, err := s.Resolve(context.Background(), buyerActor(9), ResolvePlantRequest{WfoID: " wfo-1 "})
	if err != nil || first.ScientificName != "Syzygium cumini" {
		t.Fatalf("resolve: %+v %v", first, err)
	}
	again, err := s.Resolve(context.Background(), ownerActor(8), ResolvePlantRequest{WfoID: "wfo-1"})
	if err != nil || again.ID != first.ID || len(repo.plants) != 1 {
		t.Fatalf("second resolve must reuse plant %d, got %d (%d plants)", first.ID, again.ID, len(repo.plants))
	}
}

func TestResolve_EmptyOrUnknownName(t *testing.T) {
	s := svc(newMock())
	if _, err := s.Resolve(context.Background(), ownerActor(1), ResolvePlantRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty wfo_id: want ErrInvalidInput, got %v", err)
	}
	if _, err := s.Resolve(context.Background(), ownerActor(1), ResolvePlantRequest{WfoID: "wfo-nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown wfo_id: want ErrNotFound, got %v", err)
	}
}

func TestCreate_UnknownWfoIDInvalid(t *testing.T) {
	s := svc(newMock())
	bad, good := "wfo-nope", "wfo-known"
	if _, err := s.Create(context.Background(), adminActor(1), CreatePlantRequest{ScientificName: "X y", WfoID: &bad}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown wfo_id: want ErrInvalidInput, got %v", err)
	}
	if _, err := s.Create(context.Background(), adminActor(1), CreatePlantRequest{ScientificName: "X y", WfoID: &good}); err != nil {
		t.Errorf("known wfo_id rejected: %v", err)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Fatalf("escapeLike = %q", got)
	}
}

// ─── Botanical browse (admin) ────────────────────────────────────────────────

func TestListBotanical_AdminOnlyAndNormalised(t *testing.T) {
	repo := newMock()
	s := svc(repo)
	if _, _, err := s.ListBotanical(context.Background(), ownerActor(1), ListBotanicalRequest{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner: want ErrForbidden, got %v", err)
	}
	names, page, err := s.ListBotanical(context.Background(), adminActor(1), ListBotanicalRequest{PerPage: 500, Search: "  syzygium   cumini ", Status: "accepted", Rank: " Species"})
	if err != nil || len(names) != 1 {
		t.Fatalf("admin list: %v %v", names, err)
	}
	if page.Total != 1657866 || page.PerPage != 100 || page.TotalPages != 16579 {
		t.Fatalf("pagination: %+v", page)
	}
	if in := repo.lastBrowse; in.Search != "syzygium cumini" || in.Status != "ACCEPTED" || in.Rank != "species" || in.Page != 1 {
		t.Fatalf("not normalised: %+v", in)
	}
	if _, _, err := s.ListBotanical(context.Background(), adminActor(1), ListBotanicalRequest{Status: "DOUBTFUL"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status: want ErrInvalidInput, got %v", err)
	}
}

func TestBotanicalFamilies_TotalsNames(t *testing.T) {
	resp, err := svc(newMock()).BotanicalFamilies(context.Background(), adminActor(1))
	if err != nil || resp.TotalNames != 15000 || len(resp.Families) != 2 {
		t.Fatalf("families: %+v %v", resp, err)
	}
	if _, err := svc(newMock()).BotanicalFamilies(context.Background(), buyerActor(1)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("buyer: want ErrForbidden, got %v", err)
	}
}

func TestSearchKey_MatchesSQLNormalisation(t *testing.T) {
	if got := searchKey("Malus × domestica"); got != "malus domestica" {
		t.Fatalf("searchKey = %q", got)
	}
	if got := searchKey("×Citrofortunella"); got != "citrofortunella" {
		t.Fatalf("searchKey = %q", got)
	}
}
