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

func (m *mockRepo) GetNamesByLanguage(_ context.Context, _ []int64, _ string) (map[int64]string, error) {
	return nil, nil
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
