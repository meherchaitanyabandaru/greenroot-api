package plants

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	List(ctx context.Context, input ListPlantsRequest) ([]Plant, int64, error)
	FindByID(ctx context.Context, plantID int64) (*Plant, error)
	Create(ctx context.Context, input CreatePlantRequest) (*Plant, error)
	Update(ctx context.Context, plantID int64, input UpdatePlantRequest) (*Plant, error)
	Delete(ctx context.Context, plantID int64) error
	ListSizes(ctx context.Context) ([]PlantSize, error)
	ListCategories(ctx context.Context) ([]Category, error)
	CreateCategory(ctx context.Context, name string) (Category, error)
	UpdateCategory(ctx context.Context, categoryID int64, input UpdateCategoryRequest) (Category, error)
	DeleteCategory(ctx context.Context, categoryID int64) error
	CreateImage(ctx context.Context, plantID int64, input CreateImageRequest) (*Image, error)
	GetCareGuide(ctx context.Context, plantID int64) (*CareGuide, error)
	GetNamesByLanguage(ctx context.Context, plantIDs []int64, langCode string) (map[int64]string, error)
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListPlantsRequest) ([]Plant, int64, error) {
	where, args := buildPlantWhere(input)
	countQuery := `SELECT COUNT(DISTINCT p.plant_id) FROM public.plants p ` + where

	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(`
		SELECT DISTINCT p.plant_id, p.plant_code, p.scientific_name, p.common_name, p.english_description,
			p.plant_type, p.light_requirement, p.water_requirement, p.is_active, p.created_at, p.updated_at
		FROM public.plants p
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, where, sortClause(input), len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	plants := make([]Plant, 0)
	for rows.Next() {
		plant, err := scanPlantRows(rows)
		if err != nil {
			return nil, 0, err
		}
		plants = append(plants, plant)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.attachPlantRelations(ctx, plants); err != nil {
		return nil, 0, err
	}
	return plants, total, nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, plantID int64) (*Plant, error) {
	plant, err := r.scanPlant(ctx, `WHERE p.plant_id = $1 AND p.is_active = true`, plantID)
	if err != nil {
		return nil, err
	}
	plant.Categories, _ = r.categoriesForPlant(ctx, plant.ID)
	plant.Images, _ = r.imagesForPlant(ctx, plant.ID)
	return plant, nil
}

func (r *PostgresRepository) Create(ctx context.Context, input CreatePlantRequest) (*Plant, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	plantCode, err := publiccode.Next(ctx, tx, publiccode.Plants, time.Now())
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.plants (
			plant_code, scientific_name, common_name, english_description, plant_type,
			light_requirement, water_requirement, is_active, created_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING plant_id
	`
	var plantID int64
	if err := tx.QueryRowContext(
		ctx,
		query,
		plantCode,
		input.ScientificName,
		stringOrEmpty(input.CommonName),
		stringOrEmpty(input.EnglishDescription),
		stringOrEmpty(input.PlantType),
		stringOrEmpty(input.LightRequirement),
		stringOrEmpty(input.WaterRequirement),
	).Scan(&plantID); err != nil {
		return nil, err
	}
	if err := r.replaceCategories(ctx, tx, plantID, input.CategoryIDs); err != nil {
		return nil, err
	}
	if err := r.upsertEnglishName(ctx, tx, plantID, input.CommonName, input.EnglishDescription); err != nil {
		return nil, err
	}
	if input.CareGuide != nil {
		if err := r.upsertCareGuide(ctx, tx, plantID, *input.CareGuide); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, plantID)
}

func (r *PostgresRepository) Update(ctx context.Context, plantID int64, input UpdatePlantRequest) (*Plant, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	const query = `
		UPDATE public.plants
		SET scientific_name = $2,
			common_name = NULLIF($3, ''),
			english_description = NULLIF($4, ''),
			plant_type = NULLIF($5, ''),
			light_requirement = NULLIF($6, ''),
			water_requirement = NULLIF($7, ''),
			updated_at = CURRENT_TIMESTAMP
		WHERE plant_id = $1 AND is_active = true
	`
	result, err := tx.ExecContext(
		ctx,
		query,
		plantID,
		input.ScientificName,
		stringOrEmpty(input.CommonName),
		stringOrEmpty(input.EnglishDescription),
		stringOrEmpty(input.PlantType),
		stringOrEmpty(input.LightRequirement),
		stringOrEmpty(input.WaterRequirement),
	)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	if err := r.replaceCategories(ctx, tx, plantID, input.CategoryIDs); err != nil {
		return nil, err
	}
	if err := r.upsertEnglishName(ctx, tx, plantID, input.CommonName, input.EnglishDescription); err != nil {
		return nil, err
	}
	if input.CareGuide != nil {
		if err := r.upsertCareGuide(ctx, tx, plantID, *input.CareGuide); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, plantID)
}

func (r *PostgresRepository) Delete(ctx context.Context, plantID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE public.plants SET is_active = false, updated_at = CURRENT_TIMESTAMP WHERE plant_id = $1 AND is_active = true`, plantID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListSizes(ctx context.Context) ([]PlantSize, error) {
	const query = `
		SELECT size_id, size_code, display_name, display_order, is_active
		FROM public.plant_sizes
		WHERE is_active = true
		ORDER BY display_order
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sizes := make([]PlantSize, 0)
	for rows.Next() {
		var s PlantSize
		if err := rows.Scan(&s.ID, &s.SizeCode, &s.DisplayName, &s.DisplayOrder, &s.IsActive); err != nil {
			return nil, err
		}
		sizes = append(sizes, s)
	}
	return sizes, rows.Err()
}

func (r *PostgresRepository) ListCategories(ctx context.Context) ([]Category, error) {
	const query = `
		SELECT category_id, category_name, is_active, created_at, updated_at
		FROM public.plant_categories
		ORDER BY category_name
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.ID, &category.Name, &category.IsActive, &category.CreatedAt, &category.UpdatedAt); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}
	return categories, rows.Err()
}

func (r *PostgresRepository) CreateCategory(ctx context.Context, name string) (Category, error) {
	const query = `
		INSERT INTO public.plant_categories (category_name, is_active, created_at, updated_at)
		VALUES ($1, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING category_id, category_name, is_active, created_at, updated_at
	`
	var c Category
	if err := r.db.QueryRowContext(ctx, query, name).Scan(&c.ID, &c.Name, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return Category{}, err
	}
	return c, nil
}

func (r *PostgresRepository) UpdateCategory(ctx context.Context, categoryID int64, input UpdateCategoryRequest) (Category, error) {
	const query = `
		UPDATE public.plant_categories
		SET category_name = COALESCE($2, category_name),
		    is_active      = COALESCE($3, is_active),
		    updated_at     = CURRENT_TIMESTAMP
		WHERE category_id = $1
		RETURNING category_id, category_name, is_active, created_at, updated_at
	`
	var c Category
	if err := r.db.QueryRowContext(ctx, query, categoryID, input.Name, input.IsActive).Scan(&c.ID, &c.Name, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Category{}, ErrNotFound
		}
		return Category{}, err
	}
	return c, nil
}

func (r *PostgresRepository) DeleteCategory(ctx context.Context, categoryID int64) error {
	const query = `
		UPDATE public.plant_categories
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE category_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, categoryID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) CreateImage(ctx context.Context, plantID int64, input CreateImageRequest) (*Image, error) {
	if _, err := r.FindByID(ctx, plantID); err != nil {
		return nil, err
	}
	if input.IsPrimary {
		if _, err := r.db.ExecContext(ctx, `UPDATE public.plant_images SET is_primary = false WHERE plant_id = $1`, plantID); err != nil {
			return nil, err
		}
	}
	const query = `
		INSERT INTO public.plant_images (plant_id, image_url, alt_text, display_order, is_primary, created_at, updated_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING image_id, plant_id, image_url, alt_text, display_order, is_primary, created_at, updated_at
	`
	return scanImageRow(r.db.QueryRowContext(ctx, query, plantID, input.ImageURL, stringOrEmpty(input.AltText), input.DisplayOrder, input.IsPrimary))
}

func (r *PostgresRepository) GetCareGuide(ctx context.Context, plantID int64) (*CareGuide, error) {
	const query = `
		SELECT cg.care_guide_id, p.plant_id, cg.sunlight, cg.watering, cg.soil, cg.temperature,
			cg.fertilizer, cg.pruning, COALESCE(cg.notes, p.english_description), cg.created_at, cg.updated_at
		FROM public.plants p
		LEFT JOIN public.plant_care_guides cg ON cg.plant_id = p.plant_id
		WHERE p.plant_id = $1 AND p.is_active = true
	`
	guide, err := scanCareGuideRow(r.db.QueryRowContext(ctx, query, plantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return guide, err
}


func buildPlantWhere(input ListPlantsRequest) (string, []any) {
	clauses := []string{"p.is_active = true"}
	args := make([]any, 0)
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if input.Search != "" {
		args = append(args, input.Search)
		index := len(args)
		clauses = append(clauses, fmt.Sprintf(
			`(p.plant_code ILIKE '%%' || $%d || '%%' OR p.scientific_name ILIKE '%%' || $%d || '%%' OR p.common_name ILIKE '%%' || $%d || '%%' OR p.english_description ILIKE '%%' || $%d || '%%')`,
			index,
			index,
			index,
			index,
		))
	}
	if input.CategoryID > 0 {
		add(`EXISTS (SELECT 1 FROM public.plant_category_mapping pcm WHERE pcm.plant_id = p.plant_id AND pcm.category_id = $%d)`, input.CategoryID)
	}
	if input.PlantType != "" {
		add(`p.plant_type = $%d`, input.PlantType)
	}
	if input.LightRequirement != "" {
		add(`p.light_requirement = $%d`, input.LightRequirement)
	}
	if input.WaterRequirement != "" {
		add(`p.water_requirement = $%d`, input.WaterRequirement)
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListPlantsRequest) string {
	direction := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		direction = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "id":
		return "p.plant_id " + direction
	case "plant_code":
		return "p.plant_code " + direction + " NULLS LAST, p.plant_id DESC"
	case "scientific_name":
		return "p.scientific_name " + direction + " NULLS LAST, p.plant_id DESC"
	case "common_name":
		return "p.common_name " + direction + " NULLS LAST, p.plant_id DESC"
	case "plant_type":
		return "p.plant_type " + direction + " NULLS LAST, p.plant_id DESC"
	case "light_requirement":
		return "p.light_requirement " + direction + " NULLS LAST, p.plant_id DESC"
	case "water_requirement":
		return "p.water_requirement " + direction + " NULLS LAST, p.plant_id DESC"
	case "created_at":
		return "p.created_at " + direction + " NULLS LAST, p.plant_id DESC"
	case "updated_at":
		return "p.updated_at " + direction + " NULLS LAST, p.plant_id DESC"
	default:
		return "p.plant_id DESC"
	}
}

func (r *PostgresRepository) scanPlant(ctx context.Context, where string, args ...any) (*Plant, error) {
	query := `
		SELECT p.plant_id, p.plant_code, p.scientific_name, p.common_name, p.english_description,
			p.plant_type, p.light_requirement, p.water_requirement, p.is_active, p.created_at, p.updated_at
		FROM public.plants p
		` + where
	plant, err := scanPlantRow(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return plant, err
}

func (r *PostgresRepository) replaceCategories(ctx context.Context, tx *sql.Tx, plantID int64, categoryIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM public.plant_category_mapping WHERE plant_id = $1`, plantID); err != nil {
		return err
	}
	for _, categoryID := range categoryIDs {
		if categoryID <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO public.plant_category_mapping (plant_id, category_id, created_at) VALUES ($1, $2, CURRENT_TIMESTAMP) ON CONFLICT DO NOTHING`, plantID, categoryID); err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresRepository) GetNamesByLanguage(ctx context.Context, plantIDs []int64, langCode string) (map[int64]string, error) {
	if len(plantIDs) == 0 {
		return map[int64]string{}, nil
	}
	placeholders := make([]string, len(plantIDs))
	args := make([]interface{}, len(plantIDs)+1)
	args[0] = langCode
	for i, id := range plantIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}
	query := fmt.Sprintf(`
		SELECT pn.plant_id, pn.plant_name
		FROM public.plant_names pn
		JOIN public.languages l ON l.language_id = pn.language_id
		WHERE l.language_code = $1
		  AND pn.plant_id IN (%s)
		  AND pn.is_active = true
	`, strings.Join(placeholders, ", "))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]string, len(plantIDs))
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = name
	}
	return result, rows.Err()
}

func (r *PostgresRepository) upsertEnglishName(ctx context.Context, tx *sql.Tx, plantID int64, commonName *string, description *string) error {
	name := strings.TrimSpace(stringOrEmpty(commonName))
	if name == "" {
		return nil
	}
	const query = `
		INSERT INTO public.plant_names (plant_id, language_id, plant_name, description, is_active, created_at, updated_at)
		VALUES ($1, 1, $2, NULLIF($3, ''), true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (plant_id, language_id)
		DO UPDATE SET plant_name = EXCLUDED.plant_name, description = EXCLUDED.description, updated_at = CURRENT_TIMESTAMP
	`
	_, err := tx.ExecContext(ctx, query, plantID, name, stringOrEmpty(description))
	return err
}

func (r *PostgresRepository) upsertCareGuide(ctx context.Context, tx *sql.Tx, plantID int64, input CareGuideRequest) error {
	const query = `
		INSERT INTO public.plant_care_guides (
			plant_id, sunlight, watering, soil, temperature, fertilizer, pruning, notes, created_at, updated_at
		)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (plant_id)
		DO UPDATE SET sunlight = EXCLUDED.sunlight, watering = EXCLUDED.watering, soil = EXCLUDED.soil,
			temperature = EXCLUDED.temperature, fertilizer = EXCLUDED.fertilizer, pruning = EXCLUDED.pruning,
			notes = EXCLUDED.notes, updated_at = CURRENT_TIMESTAMP
	`
	_, err := tx.ExecContext(
		ctx,
		query,
		plantID,
		stringOrEmpty(input.Sunlight),
		stringOrEmpty(input.Watering),
		stringOrEmpty(input.Soil),
		stringOrEmpty(input.Temperature),
		stringOrEmpty(input.Fertilizer),
		stringOrEmpty(input.Pruning),
		stringOrEmpty(input.Notes),
	)
	return err
}

func (r *PostgresRepository) attachPlantRelations(ctx context.Context, plants []Plant) error {
	for index := range plants {
		categories, err := r.categoriesForPlant(ctx, plants[index].ID)
		if err != nil {
			return err
		}
		images, err := r.imagesForPlant(ctx, plants[index].ID)
		if err != nil {
			return err
		}
		plants[index].Categories = categories
		plants[index].Images = images
	}
	return nil
}

func (r *PostgresRepository) categoriesForPlant(ctx context.Context, plantID int64) ([]Category, error) {
	const query = `
		SELECT c.category_id, c.category_name, c.is_active, c.created_at, c.updated_at
		FROM public.plant_category_mapping pcm
		JOIN public.plant_categories c ON c.category_id = pcm.category_id
		WHERE pcm.plant_id = $1 AND c.is_active = true
		ORDER BY c.category_name
	`
	rows, err := r.db.QueryContext(ctx, query, plantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	categories := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.ID, &category.Name, &category.IsActive, &category.CreatedAt, &category.UpdatedAt); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}
	return categories, rows.Err()
}

func (r *PostgresRepository) imagesForPlant(ctx context.Context, plantID int64) ([]Image, error) {
	const query = `
		SELECT image_id, plant_id, image_url, alt_text, display_order, is_primary, created_at, updated_at
		FROM public.plant_images
		WHERE plant_id = $1
		ORDER BY is_primary DESC, display_order ASC, image_id ASC
	`
	rows, err := r.db.QueryContext(ctx, query, plantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	images := make([]Image, 0)
	for rows.Next() {
		image, err := scanImageRows(rows)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, rows.Err()
}

func scanPlantRow(row interface{ Scan(dest ...any) error }) (*Plant, error) {
	plant, err := scanPlant(row)
	if err != nil {
		return nil, err
	}
	return &plant, nil
}

func scanPlantRows(rows *sql.Rows) (Plant, error) {
	return scanPlant(rows)
}

func scanPlant(row interface{ Scan(dest ...any) error }) (Plant, error) {
	var plant Plant
	var commonName, description, plantType, light, water sql.NullString
	if err := row.Scan(
		&plant.ID,
		&plant.PlantCode,
		&plant.ScientificName,
		&commonName,
		&description,
		&plantType,
		&light,
		&water,
		&plant.IsActive,
		&plant.CreatedAt,
		&plant.UpdatedAt,
	); err != nil {
		return Plant{}, err
	}
	plant.CommonName = nullableString(commonName)
	plant.EnglishDescription = nullableString(description)
	plant.PlantType = nullableString(plantType)
	plant.LightRequirement = nullableString(light)
	plant.WaterRequirement = nullableString(water)
	return plant, nil
}

func scanImageRow(row interface{ Scan(dest ...any) error }) (*Image, error) {
	image, err := scanImage(row)
	if err != nil {
		return nil, err
	}
	return &image, nil
}

func scanImageRows(rows *sql.Rows) (Image, error) {
	return scanImage(rows)
}

func scanImage(row interface{ Scan(dest ...any) error }) (Image, error) {
	var image Image
	var alt sql.NullString
	if err := row.Scan(&image.ID, &image.PlantID, &image.ImageURL, &alt, &image.DisplayOrder, &image.IsPrimary, &image.CreatedAt, &image.UpdatedAt); err != nil {
		return Image{}, err
	}
	image.AltText = nullableString(alt)
	return image, nil
}

func scanCareGuideRow(row interface{ Scan(dest ...any) error }) (*CareGuide, error) {
	var guide CareGuide
	var id sql.NullInt64
	var sunlight, watering, soil, temperature, fertilizer, pruning, notes sql.NullString
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&id, &guide.PlantID, &sunlight, &watering, &soil, &temperature, &fertilizer, &pruning, &notes, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if id.Valid {
		guide.ID = id.Int64
	}
	guide.Sunlight = nullableString(sunlight)
	guide.Watering = nullableString(watering)
	guide.Soil = nullableString(soil)
	guide.Temperature = nullableString(temperature)
	guide.Fertilizer = nullableString(fertilizer)
	guide.Pruning = nullableString(pruning)
	guide.Notes = nullableString(notes)
	if createdAt.Valid {
		guide.CreatedAt = &createdAt.Time
	}
	if updatedAt.Valid {
		guide.UpdatedAt = &updatedAt.Time
	}
	return &guide, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
