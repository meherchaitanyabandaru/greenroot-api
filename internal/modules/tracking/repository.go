package tracking

import (
	"context"
	"database/sql"
	"errors"
)

type Repository interface {
	Create(context.Context, CreateRequest) (*TrackingPoint, error)
	DispatchAccess(context.Context, int64) (*DispatchAccess, error)
	ListBy(context.Context, string, int64) ([]TrackingPoint, error)
	LatestBy(context.Context, string, int64) (*TrackingPoint, error)
}
type PostgresRepository struct{ db *sql.DB }

type DispatchAccess struct {
	Status       string
	DriverID     *int64
	DriverUserID *int64
}

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }
func (r *PostgresRepository) Create(ctx context.Context, in CreateRequest) (*TrackingPoint, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `INSERT INTO public.vehicle_tracking(vehicle_id,driver_id,dispatch_id,latitude,longitude,tracked_at,notes) VALUES($1,$2,$3,$4,$5,CURRENT_TIMESTAMP,NULLIF($6,'')) RETURNING tracking_id`, int64OrNil(in.VehicleID), int64OrNil(in.DriverID), int64OrNil(in.DispatchID), in.Latitude, in.Longitude, stringOrEmpty(in.Notes)).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.LatestBy(ctx, "tracking_id", id)
}
func (r *PostgresRepository) DispatchAccess(ctx context.Context, dispatchID int64) (*DispatchAccess, error) {
	var access DispatchAccess
	var driverID, driverUserID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(dispatch_status, ''), driver_id, driver_user_id
		FROM public.dispatches
		WHERE dispatch_id = $1
	`, dispatchID).Scan(&access.Status, &driverID, &driverUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidInput
	}
	if err != nil {
		return nil, err
	}
	access.DriverID = nullableInt64(driverID)
	access.DriverUserID = nullableInt64(driverUserID)
	return &access, nil
}
func (r *PostgresRepository) ListBy(ctx context.Context, col string, id int64) ([]TrackingPoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT tracking_id,vehicle_id,driver_id,dispatch_id,latitude,longitude,tracked_at,notes FROM public.vehicle_tracking WHERE `+col+`=$1 ORDER BY tracked_at DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackingPoint{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) LatestBy(ctx context.Context, col string, id int64) (*TrackingPoint, error) {
	p, err := scan(r.db.QueryRowContext(ctx, `SELECT tracking_id,vehicle_id,driver_id,dispatch_id,latitude,longitude,tracked_at,notes FROM public.vehicle_tracking WHERE `+col+`=$1 ORDER BY tracked_at DESC LIMIT 1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func scan(row interface{ Scan(...any) error }) (TrackingPoint, error) {
	var p TrackingPoint
	var v, d, di sql.NullInt64
	var notes sql.NullString
	err := row.Scan(&p.ID, &v, &d, &di, &p.Latitude, &p.Longitude, &p.TrackedAt, &notes)
	if err != nil {
		return p, err
	}
	if v.Valid {
		p.VehicleID = &v.Int64
	}
	if d.Valid {
		p.DriverID = &d.Int64
	}
	if di.Valid {
		p.DispatchID = &di.Int64
	}
	if notes.Valid {
		p.Notes = &notes.String
	}
	return p, nil
}
func int64OrNil(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func nullableInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
