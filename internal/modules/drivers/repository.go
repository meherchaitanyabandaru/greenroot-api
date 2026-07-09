package drivers

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
	List(ctx context.Context, input ListDriversRequest) ([]Driver, int64, error)
	FindByID(ctx context.Context, driverID int64) (*Driver, error)
	FindByUserID(ctx context.Context, userID int64) (*Driver, error)
	HasDuplicate(ctx context.Context, input DriverInput, excludeDriverID int64) (bool, error)
	Create(ctx context.Context, input DriverInput) (*Driver, error)
	Update(ctx context.Context, driverID int64, input DriverInput) (*Driver, error)
	Delete(ctx context.Context, driverID int64) error
	Upsert(ctx context.Context, userID int64, req ApplyDriverRequest) (*Driver, error)
	UserOwnsANursery(ctx context.Context, userID int64) (bool, error)
	Approve(ctx context.Context, driverUserID int64, approvedByUserID int64) (*Driver, error)
	CreateLocation(ctx context.Context, driverID int64, actorID int64, input LocationRequest) (*DriverLocation, error)
}

type DriverInput struct {
	UserID            *int64
	LicenseNumber     *string
	LicenseExpiryDate *time.Time
	EmergencyContact  *string
	Status            string
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListDriversRequest) ([]Driver, int64, error) {
	where, args := buildWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.drivers d LEFT JOIN public.users u ON u.user_id = d.user_id "+where, args...).Scan(&total); err != nil {
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
	drivers := make([]Driver, 0)
	for rows.Next() {
		driver, err := scanDriver(rows)
		if err != nil {
			return nil, 0, err
		}
		drivers = append(drivers, driver)
	}
	return drivers, total, rows.Err()
}

func (r *PostgresRepository) HasDuplicate(ctx context.Context, input DriverInput, excludeDriverID int64) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM public.drivers d
			WHERE d.driver_id <> $1
				AND COALESCE(d.status, '') <> 'DELETED'
				AND (
					($2::bigint IS NOT NULL AND d.user_id = $2)
					OR ($3 <> '' AND UPPER(COALESCE(d.license_number, '')) = UPPER($3))
				)
		)
	`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, excludeDriverID, int64OrNil(input.UserID), stringOrEmpty(input.LicenseNumber)).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) FindByID(ctx context.Context, driverID int64) (*Driver, error) {
	driver, err := scanDriver(r.db.QueryRowContext(ctx, baseSelect()+" WHERE d.driver_id = $1", driverID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &driver, err
}

func (r *PostgresRepository) Create(ctx context.Context, input DriverInput) (*Driver, error) {
	driverCode, err := publiccode.Next(ctx, r.db, publiccode.Drivers, time.Now())
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.drivers (driver_code, user_id, license_number, license_expiry_date, emergency_contact, status, created_at, updated_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), $6, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING driver_id
	`
	var driverID int64
	if err := r.db.QueryRowContext(ctx, query, driverCode, int64OrNil(input.UserID), stringOrEmpty(input.LicenseNumber), timeOrNil(input.LicenseExpiryDate), stringOrEmpty(input.EmergencyContact), statusOrActive(input.Status)).Scan(&driverID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, driverID)
}

func (r *PostgresRepository) Update(ctx context.Context, driverID int64, input DriverInput) (*Driver, error) {
	const query = `
		UPDATE public.drivers
		SET user_id = $2, license_number = NULLIF($3, ''), license_expiry_date = $4,
			emergency_contact = NULLIF($5, ''), status = $6, updated_at = CURRENT_TIMESTAMP
		WHERE driver_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, driverID, int64OrNil(input.UserID), stringOrEmpty(input.LicenseNumber), timeOrNil(input.LicenseExpiryDate), stringOrEmpty(input.EmergencyContact), statusOrActive(input.Status))
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, driverID)
}

func (r *PostgresRepository) Delete(ctx context.Context, driverID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE public.drivers SET status = 'INACTIVE', updated_at = CURRENT_TIMESTAMP WHERE driver_id = $1`, driverID)
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

func (r *PostgresRepository) FindByUserID(ctx context.Context, userID int64) (*Driver, error) {
	driver, err := scanDriver(r.db.QueryRowContext(ctx, baseSelect()+" WHERE d.user_id = $1 AND COALESCE(d.status::text,'') <> 'DELETED' LIMIT 1", userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &driver, err
}

func (r *PostgresRepository) UserOwnsANursery(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM public.nurseries
			WHERE owner_user_id = $1
			  AND COALESCE(status::text, '') <> 'DELETED'
		)`,
		userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) Upsert(ctx context.Context, userID int64, req ApplyDriverRequest) (*Driver, error) {
	const query = `
		INSERT INTO public.drivers (driver_code, user_id, license_number, licence_photo_url, vehicle_number, vehicle_type, profile_status, approval_status, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'COMPLETE', 'PENDING', 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (user_id) DO UPDATE
		SET license_number = EXCLUDED.license_number,
			licence_photo_url = EXCLUDED.licence_photo_url,
			vehicle_number = EXCLUDED.vehicle_number,
			vehicle_type = EXCLUDED.vehicle_type,
			profile_status = 'COMPLETE',
			updated_at = CURRENT_TIMESTAMP
		RETURNING driver_id
	`
	driverCode, err := publiccode.Next(ctx, r.db, publiccode.Drivers, time.Now())
	if err != nil {
		return nil, err
	}
	var driverID int64
	if err := r.db.QueryRowContext(ctx, query, driverCode, userID, req.LicenceNumber, req.LicencePhotoURL, req.VehicleNumber, req.VehicleType).Scan(&driverID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, driverID)
}

func (r *PostgresRepository) Approve(ctx context.Context, driverUserID int64, approvedByUserID int64) (*Driver, error) {
	const query = `
		UPDATE public.drivers
		SET approval_status = 'APPROVED', approved_by_user_id = $2, approved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE driver_id = $1 AND COALESCE(status::text, '') <> 'DELETED'
		RETURNING driver_id, user_id
	`
	var driverID int64
	var userID sql.NullInt64
	if err := r.db.QueryRowContext(ctx, query, driverUserID, approvedByUserID).Scan(&driverID, &userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Grant DRIVER role so the JWT includes it and API hasRole checks pass.
	if userID.Valid {
		_, _ = r.db.ExecContext(ctx, `
			INSERT INTO public.user_roles (user_id, role_id)
			SELECT $1, role_id FROM public.roles WHERE role_code = 'DRIVER'
			ON CONFLICT DO NOTHING
		`, userID.Int64)
	}
	return r.FindByID(ctx, driverID)
}

func (r *PostgresRepository) CreateLocation(ctx context.Context, driverID int64, actorID int64, input LocationRequest) (*DriverLocation, error) {
	const query = `
		INSERT INTO public.driver_locations (driver_id, latitude, longitude, recorded_at, created_by)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP, $4)
		RETURNING location_id, driver_id, latitude, longitude, recorded_at, created_by
	`
	location, err := scanLocation(r.db.QueryRowContext(ctx, query, driverID, input.Latitude, input.Longitude, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &location, err
}


func baseSelect() string {
	return `
		SELECT d.driver_id, d.driver_code, d.user_id, u.first_name, u.mobile, d.license_number, d.license_expiry_date,
			d.emergency_contact, COALESCE(d.status::text, ''), d.created_at, d.updated_at,
			d.licence_photo_url, d.vehicle_number, d.vehicle_type,
			COALESCE(d.profile_status, 'INCOMPLETE'), COALESCE(d.approval_status, 'PENDING'),
			d.approved_by_user_id, d.approved_at
		FROM public.drivers d
		LEFT JOIN public.users u ON u.user_id = d.user_id
	`
}

func buildWhere(input ListDriversRequest) (string, []any) {
	clauses := []string{"1 = 1"}
	args := make([]any, 0)
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("d.status::text = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(d.driver_code ILIKE $%d OR u.first_name ILIKE $%d OR u.mobile ILIKE $%d OR d.license_number ILIKE $%d)", len(args), len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListDriversRequest) string {
	direction := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		direction = "ASC"
	}

	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "id":
		return "d.driver_id " + direction
	case "driver_code":
		return "d.driver_code " + direction + " NULLS LAST, d.driver_id DESC"
	case "driver_name":
		return "u.first_name " + direction + " NULLS LAST, d.driver_id DESC"
	case "mobile":
		return "u.mobile " + direction + " NULLS LAST, d.driver_id DESC"
	case "license_number":
		return "d.license_number " + direction + " NULLS LAST, d.driver_id DESC"
	case "status":
		return "d.status " + direction + " NULLS LAST, d.driver_id DESC"
	case "created_at":
		return "d.created_at " + direction + " NULLS LAST, d.driver_id DESC"
	default:
		return "d.driver_id DESC"
	}
}

func scanDriver(row interface{ Scan(dest ...any) error }) (Driver, error) {
	var driver Driver
	var userID, approvedByUserID sql.NullInt64
	var name, mobile, license, emergency, status sql.NullString
	var licencePhoto, vehicleNumber, vehicleType sql.NullString
	var profileStatus, approvalStatus sql.NullString
	var licenseExpiry, updatedAt, approvedAt sql.NullTime
	if err := row.Scan(
		&driver.ID, &driver.DriverCode, &userID, &name, &mobile, &license, &licenseExpiry,
		&emergency, &status, &driver.CreatedAt, &updatedAt,
		&licencePhoto, &vehicleNumber, &vehicleType,
		&profileStatus, &approvalStatus,
		&approvedByUserID, &approvedAt,
	); err != nil {
		return Driver{}, err
	}
	driver.UserID = nullableInt64(userID)
	driver.DriverName = nullableString(name)
	driver.Mobile = nullableString(mobile)
	driver.LicenseNumber = nullableString(license)
	if licenseExpiry.Valid {
		driver.LicenseExpiryDate = &licenseExpiry.Time
	}
	driver.EmergencyContact = nullableString(emergency)
	driver.Status = status.String
	if updatedAt.Valid {
		driver.UpdatedAt = &updatedAt.Time
	}
	driver.LicencePhotoURL = nullableString(licencePhoto)
	driver.VehicleNumber = nullableString(vehicleNumber)
	driver.VehicleType = nullableString(vehicleType)
	driver.ProfileStatus = profileStatus.String
	driver.ApprovalStatus = approvalStatus.String
	driver.ApprovedByUserID = nullableInt64(approvedByUserID)
	if approvedAt.Valid {
		driver.ApprovedAt = &approvedAt.Time
	}
	return driver, nil
}

func scanLocation(row interface{ Scan(dest ...any) error }) (DriverLocation, error) {
	var location DriverLocation
	var createdBy sql.NullInt64
	if err := row.Scan(&location.ID, &location.DriverID, &location.Latitude, &location.Longitude, &location.RecordedAt, &createdBy); err != nil {
		return DriverLocation{}, err
	}
	location.CreatedBy = nullableInt64(createdBy)
	return location, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
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

func int64OrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func timeOrNil(value *time.Time) any {
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
