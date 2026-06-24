package vehicles

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
	List(ctx context.Context, input ListVehiclesRequest) ([]Vehicle, int64, error)
	FindByID(ctx context.Context, vehicleID int64) (*Vehicle, error)
	HasDuplicate(ctx context.Context, vehicleNumber string, excludeVehicleID int64) (bool, error)
	Create(ctx context.Context, input VehicleRequest) (*Vehicle, error)
	Update(ctx context.Context, vehicleID int64, input VehicleRequest) (*Vehicle, error)
	Delete(ctx context.Context, vehicleID int64) error
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
}

type CreateAuditInput struct {
	TableName string
	RecordID  int64
	Action    string
	ChangedBy int64
	SourceIP  string
	UserAgent string
	NewJSON   string
	At        time.Time
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListVehiclesRequest) ([]Vehicle, int64, error) {
	where, args := buildWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.vehicles "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(`
		SELECT vehicle_id, vehicle_code, vehicle_number, vehicle_type, capacity_kg, owner_name, mobile,
			COALESCE(status::text, ''), created_at, updated_at
		FROM public.vehicles
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, where, sortClause(input), len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	vehicles := make([]Vehicle, 0)
	for rows.Next() {
		vehicle, err := scanVehicle(rows)
		if err != nil {
			return nil, 0, err
		}
		vehicles = append(vehicles, vehicle)
	}
	return vehicles, total, rows.Err()
}

func (r *PostgresRepository) HasDuplicate(ctx context.Context, vehicleNumber string, excludeVehicleID int64) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM public.vehicles
			WHERE vehicle_id <> $1
				AND UPPER(vehicle_number) = UPPER($2)
		)
	`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, excludeVehicleID, strings.TrimSpace(vehicleNumber)).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) FindByID(ctx context.Context, vehicleID int64) (*Vehicle, error) {
	vehicle, err := scanVehicle(r.db.QueryRowContext(ctx, `
		SELECT vehicle_id, vehicle_code, vehicle_number, vehicle_type, capacity_kg, owner_name, mobile,
			COALESCE(status::text, ''), created_at, updated_at
		FROM public.vehicles
		WHERE vehicle_id = $1
	`, vehicleID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &vehicle, err
}

func (r *PostgresRepository) Create(ctx context.Context, input VehicleRequest) (*Vehicle, error) {
	vehicleCode, err := publiccode.Next(ctx, r.db, publiccode.Vehicles, time.Now())
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.vehicles (
			vehicle_code, vehicle_number, vehicle_type, capacity_kg, owner_name, mobile, status, created_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), $7, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING vehicle_id
	`
	var vehicleID int64
	if err := r.db.QueryRowContext(ctx, query, vehicleCode, input.VehicleNumber, stringOrEmpty(input.VehicleType), floatOrNil(input.CapacityKG), stringOrEmpty(input.OwnerName), stringOrEmpty(input.Mobile), statusOrActive(input.Status)).Scan(&vehicleID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, vehicleID)
}

func (r *PostgresRepository) Update(ctx context.Context, vehicleID int64, input VehicleRequest) (*Vehicle, error) {
	const query = `
		UPDATE public.vehicles
		SET vehicle_number = $2,
			vehicle_type = NULLIF($3, ''),
			capacity_kg = $4,
			owner_name = NULLIF($5, ''),
			mobile = NULLIF($6, ''),
			status = $7,
			updated_at = CURRENT_TIMESTAMP
		WHERE vehicle_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, vehicleID, input.VehicleNumber, stringOrEmpty(input.VehicleType), floatOrNil(input.CapacityKG), stringOrEmpty(input.OwnerName), stringOrEmpty(input.Mobile), statusOrActive(input.Status))
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, vehicleID)
}

func (r *PostgresRepository) Delete(ctx context.Context, vehicleID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE public.vehicles SET status = 'RETIRED', updated_at = CURRENT_TIMESTAMP WHERE vehicle_id = $1`, vehicleID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, input CreateAuditInput) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO public.audit_logs (table_name, record_id, action_type, old_data, new_data, changed_by, source_ip, user_agent, changed_at)
		VALUES ($1, $2, $3, NULL, NULLIF($4, '')::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
	`, input.TableName, input.RecordID, input.Action, input.NewJSON, input.ChangedBy, input.SourceIP, input.UserAgent, input.At)
	return err
}

func buildWhere(input ListVehiclesRequest) (string, []any) {
	clauses := []string{"COALESCE(status::text, '') <> 'RETIRED'"}
	args := make([]any, 0)
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("status::text = $%d", len(args)))
	}
	if input.Type != "" {
		args = append(args, input.Type)
		clauses = append(clauses, fmt.Sprintf("vehicle_type = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(vehicle_code ILIKE $%d OR vehicle_number ILIKE $%d OR owner_name ILIKE $%d OR mobile ILIKE $%d)", len(args), len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListVehiclesRequest) string {
	direction := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		direction = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "id":
		return "vehicle_id " + direction
	case "vehicle_code":
		return "vehicle_code " + direction + " NULLS LAST, vehicle_id DESC"
	case "vehicle_number":
		return "vehicle_number " + direction + " NULLS LAST, vehicle_id DESC"
	case "vehicle_type":
		return "vehicle_type " + direction + " NULLS LAST, vehicle_id DESC"
	case "owner_name":
		return "owner_name " + direction + " NULLS LAST, vehicle_id DESC"
	case "mobile":
		return "mobile " + direction + " NULLS LAST, vehicle_id DESC"
	case "status":
		return "status " + direction + " NULLS LAST, vehicle_id DESC"
	case "created_at":
		return "created_at " + direction + " NULLS LAST, vehicle_id DESC"
	default:
		return "vehicle_id DESC"
	}
}

func scanVehicle(row interface{ Scan(dest ...any) error }) (Vehicle, error) {
	var vehicle Vehicle
	var vehicleType, ownerName, mobile sql.NullString
	var capacity sql.NullFloat64
	var updatedAt sql.NullTime
	if err := row.Scan(&vehicle.ID, &vehicle.VehicleCode, &vehicle.VehicleNumber, &vehicleType, &capacity, &ownerName, &mobile, &vehicle.Status, &vehicle.CreatedAt, &updatedAt); err != nil {
		return Vehicle{}, err
	}
	vehicle.VehicleType = nullableString(vehicleType)
	vehicle.CapacityKG = nullableFloat64(capacity)
	vehicle.OwnerName = nullableString(ownerName)
	vehicle.Mobile = nullableString(mobile)
	if updatedAt.Valid {
		vehicle.UpdatedAt = &updatedAt.Time
	}
	return vehicle, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func floatOrNil(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func statusOrActive(status string) string {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		return "ACTIVE"
	}
	return status
}
