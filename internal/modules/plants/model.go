package plants

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type Plant struct {
	ID                 int64      `json:"id"`
	PlantCode          string     `json:"plant_code"`
	ScientificName     string     `json:"scientific_name"`
	CommonName         *string    `json:"common_name,omitempty"`
	EnglishDescription *string    `json:"english_description,omitempty"`
	PlantType          *string    `json:"plant_type,omitempty"`
	LightRequirement   *string    `json:"light_requirement,omitempty"`
	WaterRequirement   *string    `json:"water_requirement,omitempty"`
	IsActive           bool       `json:"is_active"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Categories         []Category `json:"categories,omitempty"`
	Images             []Image    `json:"images,omitempty"`
}

type Category struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Image struct {
	ID           int64     `json:"id"`
	PlantID      int64     `json:"plant_id"`
	ImageURL     string    `json:"image_url"`
	AltText      *string   `json:"alt_text,omitempty"`
	DisplayOrder int       `json:"display_order"`
	IsPrimary    bool      `json:"is_primary"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CareGuide struct {
	ID          int64      `json:"id"`
	PlantID     int64      `json:"plant_id"`
	Sunlight    *string    `json:"sunlight,omitempty"`
	Watering    *string    `json:"watering,omitempty"`
	Soil        *string    `json:"soil,omitempty"`
	Temperature *string    `json:"temperature,omitempty"`
	Fertilizer  *string    `json:"fertilizer,omitempty"`
	Pruning     *string    `json:"pruning,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
