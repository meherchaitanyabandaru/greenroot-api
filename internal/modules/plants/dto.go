package plants

import "github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"

type ListPlantsRequest struct {
	Page             int
	PerPage          int
	Search           string
	CategoryID       int64
	PlantType        string
	LightRequirement string
	WaterRequirement string
	SortBy           string
	SortOrder        string
	IDs              []int64 // only these plants (lookup results)
}

type CreatePlantRequest struct {
	ScientificName     string            `json:"scientific_name"`
	CommonName         *string           `json:"common_name"`
	EnglishDescription *string           `json:"english_description"`
	PlantType          *string           `json:"plant_type"`
	LightRequirement   *string           `json:"light_requirement"`
	WaterRequirement   *string           `json:"water_requirement"`
	CategoryIDs        []int64           `json:"category_ids"`
	CareGuide          *CareGuideRequest `json:"care_guide"`
	// WfoID links the plant to public.botanical_names. On update, omitted
	// keeps the current link and "" removes it.
	WfoID *string `json:"wfo_id"`
}

type UpdatePlantRequest = CreatePlantRequest

type CreateImageRequest struct {
	ImageURL     string  `json:"image_url"`
	AltText      *string `json:"alt_text"`
	DisplayOrder int     `json:"display_order"`
	IsPrimary    bool    `json:"is_primary"`
}

type CareGuideRequest struct {
	Sunlight    *string `json:"sunlight"`
	Watering    *string `json:"watering"`
	Soil        *string `json:"soil"`
	Temperature *string `json:"temperature"`
	Fertilizer  *string `json:"fertilizer"`
	Pruning     *string `json:"pruning"`
	Notes       *string `json:"notes"`
}

type Pagination = response.Pagination

type PlantsResponse struct {
	Plants     []Plant    `json:"plants"`
	Pagination Pagination `json:"pagination"`
}

type PlantResponse struct {
	Plant Plant `json:"plant"`
}

type SizesResponse struct {
	Sizes []PlantSize `json:"sizes"`
}

type CategoriesResponse struct {
	Categories []Category `json:"categories"`
}

type CategoryResponse struct {
	Category Category `json:"category"`
}

type CreateCategoryRequest struct {
	Name string `json:"name"`
}

type UpdateCategoryRequest struct {
	Name     *string `json:"name"`
	IsActive *bool   `json:"is_active"`
}

type ImageResponse struct {
	Image Image `json:"image"`
}

type CareGuideResponse struct {
	CareGuide CareGuide `json:"care_guide"`
}

type DeletePlantResponse struct {
	Message string `json:"message"`
}

type LookupRequest struct {
	Query string
	Limit int
}

type LookupResponse struct {
	Results []LookupResult `json:"results"`
}

type ResolvePlantRequest struct {
	WfoID string `json:"wfo_id"`
}
