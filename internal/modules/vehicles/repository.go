package vehicles

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
	List(ctx context.Context, input ListVehiclesRequest) ([]Vehicle, int64, error)
	FindByID(ctx context.Context, vehicleID int64) (*Vehicle, error)
	HasDuplicate(ctx context.Context, vehicleNumber string, excludeVehicleID int64) (bool, error)
	Create(ctx context.Context, input VehicleRequest) (*Vehicle, error)
	Update(ctx context.Context, vehicleID int64, input VehicleRequest) (*Vehicle, error)
	Delete(ctx context.Context, vehicleID int64) error
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
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+vehicleListFromClause()+" "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(`
		SELECT v.vehicle_id, v.vehicle_code, v.vehicle_number, v.vehicle_type, v.capacity_kg, v.owner_name, v.mobile,
			linked_driver.driver_id, linked_driver.driver_name, linked_driver.driver_mobile, linked_driver.driver_approval_status,
			COALESCE(v.status::text, ''), v.created_at, v.updated_at
		%s
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, vehicleListFromClause(), where, sortClause(input), len(args)-1, len(args))
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
	query := `
		SELECT v.vehicle_id, v.vehicle_code, v.vehicle_number, v.vehicle_type, v.capacity_kg, v.owner_name, v.mobile,
			linked_driver.driver_id, linked_driver.driver_name, linked_driver.driver_mobile, linked_driver.driver_approval_status,
			COALESCE(v.status::text, ''), v.created_at, v.updated_at
		` + vehicleListFromClause() + `
		WHERE v.vehicle_id = $1
	`
	vehicle, err := scanVehicle(r.db.QueryRowContext(ctx, query, vehicleID))
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
	const query = `
		WITH target AS (
			SELECT vehicle_id, vehicle_number
			FROM public.vehicles
			WHERE vehicle_id = $1
		),
		blocked AS (
			SELECT 1
			FROM target t
			JOIN public.drivers d
				ON UPPER(TRIM(d.vehicle_number)) = UPPER(TRIM(t.vehicle_number))
			WHERE COALESCE(d.status::text, '') = 'ACTIVE'
				AND COALESCE(d.approval_status, '') = 'APPROVED'
			LIMIT 1
		),
		updated AS (
			UPDATE public.vehicles v
			SET status = 'RETIRED', updated_at = CURRENT_TIMESTAMP
			FROM target t
			WHERE v.vehicle_id = t.vehicle_id
				AND NOT EXISTS (SELECT 1 FROM blocked)
			RETURNING v.vehicle_id
		)
		SELECT EXISTS (SELECT 1 FROM target),
			EXISTS (SELECT 1 FROM blocked),
			EXISTS (SELECT 1 FROM updated)
	`
	var exists, blocked, updated bool
	if err := r.db.QueryRowContext(ctx, query, vehicleID).Scan(&exists, &blocked, &updated); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if blocked {
		return ErrVehicleAssigned
	}
	if !updated {
		return ErrNotFound
	}
	return nil
}

func vehicleListFromClause() string {
	return `
		FROM public.vehicles v
		LEFT JOIN LATERAL (
			SELECT d.driver_id,
				NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), '') AS driver_name,
				u.mobile AS driver_mobile,
				COALESCE(d.approval_status, 'PENDING') AS driver_approval_status
			FROM public.drivers d
			LEFT JOIN public.users u ON u.user_id = d.user_id
			WHERE COALESCE(d.status::text, '') <> 'DELETED'
				AND NULLIF(TRIM(d.vehicle_number), '') IS NOT NULL
				AND UPPER(TRIM(d.vehicle_number)) = UPPER(TRIM(v.vehicle_number))
			ORDER BY CASE WHEN COALESCE(d.approval_status, '') = 'APPROVED' THEN 0 ELSE 1 END,
				d.updated_at DESC NULLS LAST,
				d.driver_id DESC
			LIMIT 1
		) linked_driver ON true
	`
}

func buildWhere(input ListVehiclesRequest) (string, []any) {
	clauses := []string{"COALESCE(v.status::text, '') <> 'RETIRED'"}
	args := make([]any, 0)
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("v.status::text = $%d", len(args)))
	}
	if input.Type != "" {
		args = append(args, input.Type)
		clauses = append(clauses, fmt.Sprintf("v.vehicle_type = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf(`(
			v.vehicle_code ILIKE $%d
			OR v.vehicle_number ILIKE $%d
			OR v.owner_name ILIKE $%d
			OR v.mobile ILIKE $%d
			OR linked_driver.driver_name ILIKE $%d
			OR linked_driver.driver_mobile ILIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args), len(args)))
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
		return "v.vehicle_id " + direction
	case "vehicle_code":
		return "v.vehicle_code " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "vehicle_number":
		return "v.vehicle_number " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "vehicle_type":
		return "v.vehicle_type " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "owner_name":
		return "v.owner_name " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "mobile":
		return "v.mobile " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "driver_name":
		return "linked_driver.driver_name " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "driver_mobile":
		return "linked_driver.driver_mobile " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "status":
		return "v.status " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "created_at":
		return "v.created_at " + direction + " NULLS LAST, v.vehicle_id DESC"
	default:
		return "v.vehicle_id DESC"
	}
}

func scanVehicle(row interface{ Scan(dest ...any) error }) (Vehicle, error) {
	var vehicle Vehicle
	var driverID sql.NullInt64
	var vehicleType, ownerName, mobile sql.NullString
	var driverName, driverMobile, driverApprovalStatus sql.NullString
	var capacity sql.NullFloat64
	var updatedAt sql.NullTime
	if err := row.Scan(
		&vehicle.ID, &vehicle.VehicleCode, &vehicle.VehicleNumber, &vehicleType, &capacity, &ownerName, &mobile,
		&driverID, &driverName, &driverMobile, &driverApprovalStatus,
		&vehicle.Status, &vehicle.CreatedAt, &updatedAt,
	); err != nil {
		return Vehicle{}, err
	}
	vehicle.VehicleType = nullableString(vehicleType)
	vehicle.CapacityKG = nullableFloat64(capacity)
	vehicle.OwnerName = nullableString(ownerName)
	vehicle.Mobile = nullableString(mobile)
	vehicle.DriverID = nullableInt64(driverID)
	vehicle.DriverName = nullableString(driverName)
	vehicle.DriverMobile = nullableString(driverMobile)
	vehicle.DriverApprovalStatus = nullableString(driverApprovalStatus)
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

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
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
