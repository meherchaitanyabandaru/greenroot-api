package tracking

import (
	"context"
	"database/sql"
	"errors"
)

type Repository interface {
	Create(context.Context, CreateRequest) (*TrackingPoint, error)
	DispatchAccess(context.Context, int64) (*DispatchAccess, error)
	ListBy(context.Context, Subject, int64) ([]TrackingPoint, error)
	LatestBy(context.Context, Subject, int64) (*TrackingPoint, error)
	CanViewDriver(ctx context.Context, driverID, userID int64) (bool, error)
	CanViewLiveDriver(ctx context.Context, driverUserID, userID int64) (bool, error)
	CanViewDispatch(ctx context.Context, dispatchID, userID int64) (bool, error)
}

// Subject is the vehicle_tracking column a query filters on. Only these
// values exist, so a column name can never come from request input.
type Subject string

const (
	SubjectVehicle  Subject = "vehicle_id"
	SubjectDriver   Subject = "driver_id"
	SubjectDispatch Subject = "dispatch_id"
	subjectPoint    Subject = "tracking_id"
)

func (s Subject) column() (string, error) {
	switch s {
	case SubjectVehicle, SubjectDriver, SubjectDispatch, subjectPoint:
		return string(s), nil
	}
	return "", ErrInvalidInput
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
	err := r.db.QueryRowContext(ctx, `INSERT INTO public.vehicle_tracking(vehicle_id,driver_id,dispatch_id,latitude,longitude,tracked_at,notes) VALUES($1,$2,$3,$4,$5,COALESCE($7::timestamptz,CURRENT_TIMESTAMP),NULLIF($6,'')) RETURNING tracking_id`, int64OrNil(in.VehicleID), int64OrNil(in.DriverID), int64OrNil(in.DispatchID), in.Latitude, in.Longitude, stringOrEmpty(in.Notes), in.RecordedAt).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.LatestBy(ctx, subjectPoint, id)
}
func (r *PostgresRepository) DispatchAccess(ctx context.Context, dispatchID int64) (*DispatchAccess, error) {
	var access DispatchAccess
	var driverID, driverUserID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(status, ''), driver_id, driver_user_id
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
func (r *PostgresRepository) ListBy(ctx context.Context, subject Subject, id int64) ([]TrackingPoint, error) {
	col, err := subject.column()
	if err != nil {
		return nil, err
	}
	// #nosec G202 -- col comes from Subject.column(), a closed allowlist.
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
func (r *PostgresRepository) LatestBy(ctx context.Context, subject Subject, id int64) (*TrackingPoint, error) {
	col, err := subject.column()
	if err != nil {
		return nil, err
	}
	// #nosec G202 -- col comes from Subject.column(), a closed allowlist.
	p, err := scan(r.db.QueryRowContext(ctx, `SELECT tracking_id,vehicle_id,driver_id,dispatch_id,latitude,longitude,tracked_at,notes FROM public.vehicle_tracking WHERE `+col+`=$1 ORDER BY tracked_at DESC LIMIT 1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// nurseryMember matches the same people as dispatches.IsNurseryMember: an
// active nursery_users member, or the owner of a nursery that is not deleted.
// $2 is the viewer; nurseryExpr names the nursery id column.
func nurseryMember(nurseryExpr string) string {
	return `EXISTS (
		SELECT 1 FROM public.nursery_users nu
		WHERE nu.nursery_id = ` + nurseryExpr + ` AND nu.user_id = $2 AND COALESCE(nu.is_active, true) = true
		UNION ALL
		SELECT 1 FROM public.nurseries n
		WHERE n.nursery_id = ` + nurseryExpr + ` AND n.owner_user_id = $2 AND COALESCE(n.status::text, '') <> 'DELETED')`
}

// CanViewDriver: the driver themself, or staff of a nursery whose trip the
// driver is running now. Drivers belong to no nursery; the trip is the link.
func (r *PostgresRepository) CanViewDriver(ctx context.Context, driverID, userID int64) (bool, error) {
	var ok bool
	// #nosec G202 -- nurseryMember only interpolates a fixed column name.
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.drivers dr
			WHERE dr.driver_id = $1 AND (
				dr.user_id = $2 OR `+onTripOfViewersNursery("dr.user_id")+`))`, driverID, userID).Scan(&ok)
	return ok, err
}

// CanViewLiveDriver is CanViewDriver keyed by the driver's user id.
func (r *PostgresRepository) CanViewLiveDriver(ctx context.Context, driverUserID, userID int64) (bool, error) {
	var ok bool
	// #nosec G202 -- nurseryMember only interpolates a fixed column name.
	err := r.db.QueryRowContext(ctx, `SELECT $1 = $2 OR `+onTripOfViewersNursery("$1")+``,
		driverUserID, userID).Scan(&ok)
	return ok, err
}

// onTripOfViewersNursery: the driver ($driverExpr) has an active trip for a
// nursery the viewer ($2) runs.
func onTripOfViewersNursery(driverExpr string) string {
	return `EXISTS (
		SELECT 1 FROM public.dispatches d
		JOIN public.orders o ON o.order_id = d.order_id
		WHERE d.driver_user_id = ` + driverExpr + `
		  AND d.status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')
		  AND ` + nurseryMember("o.seller_nursery_id") + `)`
}

// CanViewDispatch mirrors dispatches.Service.canAccess: the trip's driver,
// the order's buyer, or staff of the selling nursery.
func (r *PostgresRepository) CanViewDispatch(ctx context.Context, dispatchID, userID int64) (bool, error) {
	var ok bool
	// #nosec G202 -- nurseryMember only interpolates a fixed column name.
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.dispatches d
			JOIN public.orders o ON o.order_id = d.order_id
			LEFT JOIN public.drivers dr ON dr.driver_id = d.driver_id
			WHERE d.dispatch_id = $1 AND (
				d.driver_user_id = $2 OR dr.user_id = $2 OR o.buyer_user_id = $2
				OR `+nurseryMember("o.seller_nursery_id")+`))`, dispatchID, userID).Scan(&ok)
	return ok, err
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
