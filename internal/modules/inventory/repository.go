package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var ErrNotFound     = apperrs.ErrNotFound

type Repository interface {
	List(ctx context.Context, input ListInventoryRequest) ([]InventoryItem, int64, error)
	FindByID(ctx context.Context, inventoryID int64) (*InventoryItem, error)
	Create(ctx context.Context, actorID int64, input UpsertInventoryRequest) (*InventoryItem, error)
	Update(ctx context.Context, actorID int64, inventoryID int64, input UpsertInventoryRequest) (*InventoryItem, error)
	Delete(ctx context.Context, inventoryID int64) error
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListInventoryRequest) ([]InventoryItem, int64, error) {
	where, args := buildWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, baseCount()+` `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(baseSelect()+`
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, where, sortClause(input), len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]InventoryItem, 0)
	for rows.Next() {
		item, err := scanInventoryRows(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, inventoryID int64) (*InventoryItem, error) {
	item, err := scanInventoryRow(r.db.QueryRowContext(ctx, baseSelect()+` WHERE ni.inventory_id = $1`, inventoryID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input UpsertInventoryRequest) (*InventoryItem, error) {
	inventoryCode, err := publiccode.Next(ctx, r.db, publiccode.Inventory, time.Now())
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.nursery_inventory (
			inventory_code, nursery_id, plant_id, size_id, available_quantity, inventory_status,
			last_updated_by, last_updated_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING inventory_id
	`
	var inventoryID int64
	if err := r.db.QueryRowContext(ctx, query, inventoryCode, input.NurseryID, input.PlantID, input.SizeID, input.AvailableQuantity, input.Status, actorID).Scan(&inventoryID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, inventoryID)
}

func (r *PostgresRepository) Update(ctx context.Context, actorID int64, inventoryID int64, input UpsertInventoryRequest) (*InventoryItem, error) {
	const query = `
		UPDATE public.nursery_inventory
		SET nursery_id = $2,
			plant_id = $3,
			size_id = $4,
			available_quantity = $5,
			inventory_status = $6,
			last_updated_by = $7,
			last_updated_at = CURRENT_TIMESTAMP
		WHERE inventory_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, inventoryID, input.NurseryID, input.PlantID, input.SizeID, input.AvailableQuantity, input.Status, actorID)
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
	return r.FindByID(ctx, inventoryID)
}

func (r *PostgresRepository) Delete(ctx context.Context, inventoryID int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM public.nursery_inventory WHERE inventory_id = $1`, inventoryID)
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

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM public.nursery_users
		WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true
		UNION ALL
		SELECT 1 FROM public.nurseries
		WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text, '') <> 'DELETED'
	)`, nurseryID, userID).Scan(&exists)
	return exists, err
}


func baseSelect() string {
	return `
		SELECT ni.inventory_id, ni.inventory_code, ni.nursery_id, n.nursery_name, ni.plant_id, p.scientific_name,
			p.common_name, ni.size_id, ps.size_code, ps.display_name, ni.available_quantity,
			ni.inventory_status::text, ni.last_updated_by, ni.last_updated_at, ni.created_at
		FROM public.nursery_inventory ni
		JOIN public.nurseries n ON n.nursery_id = ni.nursery_id
		JOIN public.plants p ON p.plant_id = ni.plant_id
		JOIN public.plant_sizes ps ON ps.size_id = ni.size_id
	`
}

func baseCount() string {
	return `
		SELECT COUNT(*)
		FROM public.nursery_inventory ni
		JOIN public.nurseries n ON n.nursery_id = ni.nursery_id
		JOIN public.plants p ON p.plant_id = ni.plant_id
		JOIN public.plant_sizes ps ON ps.size_id = ni.size_id
	`
}

func buildWhere(input ListInventoryRequest) (string, []any) {
	clauses := []string{"COALESCE(n.status::text, '') <> 'DELETED'", "p.is_active = true"}
	args := make([]any, 0)
	if input.NurseryID > 0 {
		args = append(args, input.NurseryID)
		clauses = append(clauses, fmt.Sprintf("ni.nursery_id = $%d", len(args)))
	}
	if input.PlantID > 0 {
		args = append(args, input.PlantID)
		clauses = append(clauses, fmt.Sprintf("ni.plant_id = $%d", len(args)))
	}
	if input.SizeID > 0 {
		args = append(args, input.SizeID)
		clauses = append(clauses, fmt.Sprintf("ni.size_id = $%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("ni.inventory_status::text = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(ni.inventory_code ILIKE $%d OR n.nursery_name ILIKE $%d OR p.scientific_name ILIKE $%d OR p.common_name ILIKE $%d OR ps.size_code ILIKE $%d OR ps.display_name ILIKE $%d)", len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListInventoryRequest) string {
	direction := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		direction = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "id":
		return "ni.inventory_id " + direction
	case "inventory_code":
		return "ni.inventory_code " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "nursery_name":
		return "n.nursery_name " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "scientific_name":
		return "p.scientific_name " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "common_name":
		return "p.common_name " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "size_code", "size_name":
		return "ps.size_code " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "available_quantity":
		return "ni.available_quantity " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "inventory_status", "status":
		return "ni.inventory_status " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "last_updated_at":
		return "ni.last_updated_at " + direction + " NULLS LAST, ni.inventory_id DESC"
	case "created_at":
		return "ni.created_at " + direction + " NULLS LAST, ni.inventory_id DESC"
	default:
		return "ni.inventory_id DESC"
	}
}

func scanInventoryRow(row interface{ Scan(dest ...any) error }) (*InventoryItem, error) {
	item, err := scanInventory(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanInventoryRows(rows *sql.Rows) (InventoryItem, error) {
	return scanInventory(rows)
}

func scanInventory(row interface{ Scan(dest ...any) error }) (InventoryItem, error) {
	var item InventoryItem
	var commonName sql.NullString
	var lastUpdatedBy sql.NullInt64
	if err := row.Scan(
		&item.ID,
		&item.InventoryCode,
		&item.NurseryID,
		&item.NurseryName,
		&item.PlantID,
		&item.ScientificName,
		&commonName,
		&item.SizeID,
		&item.SizeCode,
		&item.SizeName,
		&item.AvailableQuantity,
		&item.Status,
		&lastUpdatedBy,
		&item.LastUpdatedAt,
		&item.CreatedAt,
	); err != nil {
		return InventoryItem{}, err
	}
	if commonName.Valid {
		item.CommonName = &commonName.String
	}
	if lastUpdatedBy.Valid {
		item.LastUpdatedBy = &lastUpdatedBy.Int64
	}
	return item, nil
}
