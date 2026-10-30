package vehicles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

var ErrNotFound = apperrs.ErrNotFound

type Repository interface {
	List(ctx context.Context, input ListVehiclesRequest) ([]Vehicle, int64, error)
	FindByID(ctx context.Context, vehicleID int64) (*Vehicle, error)
	HasDuplicate(ctx context.Context, vehicleNumber string, excludeVehicleID int64) (bool, error)
	Create(ctx context.Context, input VehicleRequest) (*Vehicle, error)
	Update(ctx context.Context, vehicleID int64, input VehicleRequest) (*Vehicle, error)
	Delete(ctx context.Context, vehicleID int64) error
	ListOwners(ctx context.Context, input ListOwnersRequest) ([]VehicleOwner, int64, error)
	FindOwner(ctx context.Context, id int64) (VehicleOwner, error)
	SaveOwner(ctx context.Context, actorID, id int64, req OwnerRequest) (VehicleOwner, error)
	SetApproval(ctx context.Context, actorID, vehicleID int64, status string, note *string) (*Vehicle, []int64, error)
	ListLinks(ctx context.Context, input ListLinksRequest) ([]VehicleDriverLink, int64, error)
	FindLink(ctx context.Context, id int64) (VehicleDriverLink, error)
	SetLinkStatus(ctx context.Context, actorID, id int64, from, to string, note *string) (VehicleDriverLink, error)
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
	// #nosec G201 -- WHERE/ORDER BY fragments are fixed strings with $n placeholders (ORDER BY from an allowlist); values are bound args.
	query := fmt.Sprintf(`
		SELECT `+vehicleColumns+`
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
		SELECT ` + vehicleColumns + `
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
			vehicle_code, vehicle_number, vehicle_type, capacity_kg, owner_id, status, approval_status, approved_at, created_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, 'APPROVED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING vehicle_id
	`
	// A lorry GreenRoot registers itself starts approved.
	var vehicleID int64
	if err := r.db.QueryRowContext(ctx, query, vehicleCode, input.VehicleNumber, stringOrEmpty(input.VehicleType), floatOrNil(input.CapacityKG), int64OrNil(input.OwnerID), statusOrActive(input.Status)).Scan(&vehicleID); err != nil {
		return nil, pgerr.Map(err)
	}
	return r.FindByID(ctx, vehicleID)
}

func (r *PostgresRepository) Update(ctx context.Context, vehicleID int64, input VehicleRequest) (*Vehicle, error) {
	const query = `
		UPDATE public.vehicles
		SET vehicle_number = $2,
			vehicle_type = NULLIF($3, ''),
			capacity_kg = $4,
			owner_id = $5,
			status = $6,
			updated_at = CURRENT_TIMESTAMP
		WHERE vehicle_id = $1
		  AND NOT ($6 <> 'ACTIVE' AND status = 'ACTIVE' AND EXISTS (
		      SELECT 1 FROM public.dispatches t WHERE t.vehicle_id = $1 AND t.status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')))
	`
	// A lorry on a trip stays ACTIVE until the trip ends.
	result, err := r.db.ExecContext(ctx, query, vehicleID, input.VehicleNumber, stringOrEmpty(input.VehicleType), floatOrNil(input.CapacityKG), int64OrNil(input.OwnerID), statusOrActive(input.Status))
	if err != nil {
		return nil, pgerr.Map(err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		if _, err := r.FindByID(ctx, vehicleID); err != nil {
			return nil, err
		}
		return nil, ErrVehicleOnTrip
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
			FROM public.dispatches d
			WHERE d.vehicle_id = $1 AND d.status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')
			LIMIT 1
		),
		updated AS (
			UPDATE public.vehicles v
			SET status = 'RETIRED', updated_at = CURRENT_TIMESTAMP
			FROM target t
			WHERE v.vehicle_id = t.vehicle_id
				AND NOT EXISTS (SELECT 1 FROM blocked)
			RETURNING v.vehicle_id
		),
		-- A retired lorry takes no more trips: its open driver links end.
		unlinked AS (
			UPDATE public.vehicle_drivers vd
			SET status = 'REMOVED', removed_at = CURRENT_TIMESTAMP, decision_note = 'lorry retired', updated_at = CURRENT_TIMESTAMP
			FROM updated u
			WHERE vd.vehicle_id = u.vehicle_id AND vd.status IN ('PENDING', 'APPROVED', 'SUSPENDED')
			RETURNING vd.vehicle_driver_id
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
		return ErrVehicleOnTrip
	}
	if !updated {
		return ErrNotFound
	}
	return nil
}

// vehicleColumns is what scanVehicle reads. The owner comes from
// vehicle_owners (falling back to the deprecated columns); the driver shown is
// the most recent approved link.
const vehicleColumns = `v.vehicle_id, v.vehicle_code, v.vehicle_number, v.vehicle_type, v.capacity_kg,
			v.owner_id, COALESCE(o.name, v.owner_name), COALESCE(o.mobile, v.mobile), o.user_id IS NOT NULL,
			linked_driver.driver_id, linked_driver.driver_name, linked_driver.driver_mobile, linked_driver.driver_approval_status,
			COALESCE(v.status::text, ''), v.approval_status, v.approval_note,
			(SELECT COUNT(*) FROM public.vehicle_drivers c WHERE c.vehicle_id = v.vehicle_id AND c.status = 'APPROVED'),
			EXISTS (SELECT 1 FROM public.dispatches t WHERE t.vehicle_id = v.vehicle_id AND t.status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')),
			v.created_at, v.updated_at`

func vehicleListFromClause() string {
	return `
		FROM public.vehicles v
		LEFT JOIN public.vehicle_owners o ON o.vehicle_owner_id = v.owner_id
		LEFT JOIN LATERAL (
			SELECT d.driver_id,
				NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), '') AS driver_name,
				u.mobile AS driver_mobile,
				COALESCE(d.approval_status, 'PENDING') AS driver_approval_status
			FROM public.vehicle_drivers vd
			JOIN public.users u ON u.user_id = vd.driver_user_id
			LEFT JOIN public.drivers d ON d.user_id = vd.driver_user_id
			WHERE vd.vehicle_id = v.vehicle_id AND vd.status = 'APPROVED'
			ORDER BY vd.decided_at DESC NULLS LAST, vd.vehicle_driver_id DESC
			LIMIT 1
		) linked_driver ON true
	`
}

func buildWhere(input ListVehiclesRequest) (string, []any) {
	clauses := []string{"COALESCE(v.status::text, '') <> 'RETIRED'"}
	args := make([]any, 0)
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("v.status::text = ANY(string_to_array($%d::text, ','))", len(args)))
	}
	if input.Type != "" {
		args = append(args, input.Type)
		clauses = append(clauses, fmt.Sprintf("v.vehicle_type::text = ANY(string_to_array($%d::text, ','))", len(args)))
	}
	if input.OwnerID > 0 {
		args = append(args, input.OwnerID)
		clauses = append(clauses, fmt.Sprintf("v.owner_id = $%d", len(args)))
	}
	if input.Approval != "" {
		args = append(args, input.Approval)
		clauses = append(clauses, fmt.Sprintf("v.approval_status = ANY(string_to_array($%d::text, ','))", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf(`(
			v.vehicle_code ILIKE $%d
			OR v.vehicle_number ILIKE $%d
			OR o.name ILIKE $%d
			OR o.mobile ILIKE $%d
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
		return "o.name " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "mobile":
		return "o.mobile " + direction + " NULLS LAST, v.vehicle_id DESC"
	case "approval_status":
		return "v.approval_status " + direction + ", v.vehicle_id DESC"
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
	var driverID, ownerID sql.NullInt64
	var vehicleType, ownerName, mobile, approvalNote sql.NullString
	var driverName, driverMobile, driverApprovalStatus sql.NullString
	var capacity sql.NullFloat64
	var updatedAt sql.NullTime
	if err := row.Scan(
		&vehicle.ID, &vehicle.VehicleCode, &vehicle.VehicleNumber, &vehicleType, &capacity,
		&ownerID, &ownerName, &mobile, &vehicle.OwnerHasAccount,
		&driverID, &driverName, &driverMobile, &driverApprovalStatus,
		&vehicle.Status, &vehicle.ApprovalStatus, &approvalNote, &vehicle.DriverCount, &vehicle.IsOnActiveTrip,
		&vehicle.CreatedAt, &updatedAt,
	); err != nil {
		return Vehicle{}, err
	}
	vehicle.OwnerID = nullableInt64(ownerID)
	vehicle.ApprovalNote = nullableString(approvalNote)
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

func int64OrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
