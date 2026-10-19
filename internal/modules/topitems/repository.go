package topitems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/rowversion"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

var (
	ErrNotFound = apperrs.ErrNotFound
	ErrConflict = apperrs.ErrConflict
)

const uniqueItem = "nursery_top_items_nursery_id_plant_id_size_id_key"

type Repository interface {
	List(ctx context.Context, input ListRequest) ([]TopItem, int64, error)
	FindByID(ctx context.Context, id int64) (*TopItem, error)
	Create(ctx context.Context, actorID int64, input CreateRequest) (*TopItem, error)
	Update(ctx context.Context, actorID int64, id int64, input UpdateRequest) (*TopItem, error)
	Delete(ctx context.Context, id int64) error
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListRequest) ([]TopItem, int64, error) {
	where, args := buildWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, baseCount()+` `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, input.PerPage, (input.Page-1)*input.PerPage)
	query := fmt.Sprintf(baseSelect()+` %s ORDER BY %s LIMIT $%d OFFSET $%d`, where, sortClause(input), len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]TopItem, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, id int64) (*TopItem, error) {
	item, err := scan(r.db.QueryRowContext(ctx, baseSelect()+` WHERE ti.top_item_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// Create locks the nursery row so two parallel adds cannot pass the
// MaxPerNursery check together. A missing display_order appends at the end.
func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input CreateRequest) (*TopItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var locked int64
	err = tx.QueryRowContext(ctx,
		`SELECT nursery_id FROM public.nurseries WHERE nursery_id = $1 AND status <> 'DELETED' FOR UPDATE`, input.NurseryID,
	).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var count int16
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM public.nursery_top_items WHERE nursery_id = $1`, input.NurseryID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxPerNursery {
		return nil, ErrLimitReached
	}
	order := count
	if input.DisplayOrder != nil {
		order = *input.DisplayOrder
	}
	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.nursery_top_items (nursery_id, plant_id, size_id, unit_price, display_order, quality_notes, photos, updated_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING top_item_id`,
		input.NurseryID, input.PlantID, input.SizeID, input.UnitPrice, order, input.QualityNotes, photosJSON(input.Photos), actorID,
	).Scan(&id)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) Update(ctx context.Context, actorID int64, id int64, input UpdateRequest) (*TopItem, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.nursery_top_items
		SET plant_id = $2, size_id = $3, unit_price = $4,
			display_order = COALESCE($5, display_order),
			quality_notes = $6, photos = $7,
			updated_by_user_id = $8, updated_at = CURRENT_TIMESTAMP
		WHERE top_item_id = $1
		  AND ($9::bigint IS NULL OR version = $9)`,
		id, input.PlantID, input.SizeID, input.UnitPrice, input.DisplayOrder, input.QualityNotes, photosJSON(input.Photos), actorID, rowversion.Arg(ctx))
	if err != nil {
		return nil, mapWriteErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		// The guarded UPDATE touched nothing: gone (404) or a newer version (409).
		if _, err := r.FindByID(ctx, id); err != nil {
			return nil, err
		}
		return nil, ErrConflict
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM public.nursery_top_items WHERE top_item_id = $1`, id)
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

// IsNurseryMember: the owner, or an active manager, of the nursery.
func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var member bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND owner_user_id = $2 AND status <> 'DELETED'
		UNION ALL
		SELECT 1 FROM public.nursery_users WHERE nursery_id = $1 AND user_id = $2 AND status = 'ACTIVE'
	)`, nurseryID, userID).Scan(&member)
	return member, err
}

// photosJSON stores a missing list as [] (never JSON null).
func photosJSON(photos []string) string {
	if photos == nil {
		photos = []string{}
	}
	b, _ := json.Marshal(photos)
	return string(b)
}

func mapWriteErr(err error) error {
	if pgerr.Constraint(err) == uniqueItem {
		return ErrDuplicate
	}
	return pgerr.Map(err)
}

const fromJoins = `
		FROM public.nursery_top_items ti
		JOIN public.nurseries n ON n.nursery_id = ti.nursery_id
		JOIN public.plants p ON p.plant_id = ti.plant_id
		JOIN public.plant_sizes ps ON ps.size_id = ti.size_id`

func baseSelect() string {
	return `
		SELECT ti.top_item_id, ti.top_item_code, ti.nursery_id, n.nursery_name, ti.plant_id, p.scientific_name,
			p.common_name, ti.size_id, ps.size_code, ps.display_name, ti.unit_price, ti.display_order,
			ti.quality_notes, ti.photos, ti.updated_by_user_id, ti.updated_at, ti.created_at, ti.version` + fromJoins
}

func baseCount() string {
	return `SELECT COUNT(*)` + fromJoins
}

func buildWhere(input ListRequest) (string, []any) {
	clauses := []string{"n.status <> 'DELETED'", "p.is_active = true"}
	args := make([]any, 0)
	if input.NurseryID > 0 {
		args = append(args, input.NurseryID)
		clauses = append(clauses, fmt.Sprintf("ti.nursery_id = $%d", len(args)))
	}
	if input.PlantID > 0 {
		args = append(args, input.PlantID)
		clauses = append(clauses, fmt.Sprintf("ti.plant_id = $%d", len(args)))
	}
	if input.SizeID > 0 {
		args = append(args, input.SizeID)
		clauses = append(clauses, fmt.Sprintf("ti.size_id = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf("(ti.top_item_code ILIKE $%d OR n.nursery_name ILIKE $%d OR p.scientific_name ILIKE $%d OR p.common_name ILIKE $%d)", n, n, n, n))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

// sortClause defaults to the showcase order: nursery, then its display order.
func sortClause(input ListRequest) string {
	direction := "ASC"
	if input.SortOrder == "desc" {
		direction = "DESC"
	}
	switch strings.ToLower(input.SortBy) {
	case "scientific_name":
		return "p.scientific_name " + direction + ", ti.top_item_id"
	case "unit_price":
		return "ti.unit_price " + direction + " NULLS LAST, ti.top_item_id"
	case "updated_at":
		return "ti.updated_at " + direction + ", ti.top_item_id"
	case "created_at":
		return "ti.created_at " + direction + ", ti.top_item_id"
	case "nursery_name":
		return "n.nursery_name " + direction + ", ti.display_order, ti.top_item_id"
	default:
		return "n.nursery_name, ti.nursery_id, ti.display_order, ti.top_item_id"
	}
}

func scan(row interface{ Scan(dest ...any) error }) (TopItem, error) {
	var item TopItem
	var commonName sql.NullString
	var price sql.NullFloat64
	var updatedBy sql.NullInt64
	var notes sql.NullString
	var photos []byte
	if err := row.Scan(&item.ID, &item.TopItemCode, &item.NurseryID, &item.NurseryName, &item.PlantID, &item.ScientificName,
		&commonName, &item.SizeID, &item.SizeCode, &item.SizeName, &price, &item.DisplayOrder,
		&notes, &photos, &updatedBy, &item.UpdatedAt, &item.CreatedAt, &item.Version); err != nil {
		return TopItem{}, err
	}
	if commonName.Valid {
		item.CommonName = &commonName.String
	}
	if price.Valid {
		item.UnitPrice = &price.Float64
	}
	if updatedBy.Valid {
		item.UpdatedByUserID = &updatedBy.Int64
	}
	if notes.Valid {
		item.QualityNotes = &notes.String
	}
	if err := json.Unmarshal(photos, &item.Photos); err != nil || item.Photos == nil {
		item.Photos = []string{}
	}
	return item, nil
}
