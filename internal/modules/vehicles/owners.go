package vehicles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/phone"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

// Lorry owners, GreenRoot's approval of each lorry, and of each driver's link
// to a lorry. Admin only. An owner needs no GreenRoot account; drivers ask to
// be linked from the app (POST /drivers/me/vehicles).

var (
	ErrVehicleOnTrip   = errors.New("lorry is on an active trip")
	ErrInvalidStatus   = errors.New("status change not allowed")
	ErrDuplicateOwner  = errors.New("an owner with this mobile already exists")
	ErrInvalidMobile   = errors.New("invalid mobile number")
	auditEntityOwner   = "vehicle_owner"
	auditEntityVehLink = "vehicle_driver"
)

type VehicleOwner struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Mobile       *string   `json:"mobile,omitempty"`
	UserID       *int64    `json:"user_id,omitempty"`
	HasAccount   bool      `json:"has_account"`
	Notes        *string   `json:"notes,omitempty"`
	VehicleCount int       `json:"vehicle_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type OwnerRequest struct {
	Name   string  `json:"name"`
	Mobile *string `json:"mobile"`
	Notes  *string `json:"notes"`
}

// ApprovalRequest decides on a lorry: APPROVED, REJECTED or SUSPENDED (or back to PENDING).
type ApprovalRequest struct {
	Status string  `json:"status"`
	Note   *string `json:"note"`
}

// VehicleDriverLink is one driver's link to one lorry, as admins review it.
type VehicleDriverLink struct {
	ID              int64      `json:"id"`
	VehicleID       int64      `json:"vehicle_id"`
	VehicleNumber   string     `json:"vehicle_number"`
	VehicleType     *string    `json:"vehicle_type,omitempty"`
	VehicleApproval string     `json:"vehicle_approval_status"`
	OwnerName       *string    `json:"owner_name,omitempty"`
	DriverUserID    int64      `json:"driver_user_id"`
	DriverName      *string    `json:"driver_name,omitempty"`
	DriverMobile    *string    `json:"driver_mobile,omitempty"`
	DriverApproval  *string    `json:"driver_approval_status,omitempty"`
	LicenceExpiry   *time.Time `json:"license_expiry_date,omitempty"`
	Status          string     `json:"status"`
	DecisionNote    *string    `json:"decision_note,omitempty"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	IsOnActiveTrip  bool       `json:"is_on_active_trip"`
	CreatedAt       time.Time  `json:"created_at"`
}

type ListLinksRequest struct {
	Page, PerPage int
	Status        string
	VehicleID     int64
	DriverUserID  int64
	Search        string
}

type ListOwnersRequest struct {
	Page, PerPage int
	Search        string
}

// LinkStatusRequest moves a link: PENDING → APPROVED/REJECTED,
// APPROVED ↔ SUSPENDED, APPROVED/SUSPENDED/PENDING → REMOVED.
type LinkStatusRequest struct {
	Status string  `json:"status"`
	Note   *string `json:"note"`
}

var linkTransitions = map[string][]string{
	"PENDING":   {"APPROVED", "REJECTED", "REMOVED"},
	"APPROVED":  {"SUSPENDED", "REMOVED"},
	"SUSPENDED": {"APPROVED", "REMOVED"},
}

func linkTransitionAllowed(from, to string) bool {
	for _, s := range linkTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func isAdmin(actor ActorContext) bool { return actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") }

func normalizeOwner(req OwnerRequest) (OwnerRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 255 {
		return req, ErrInvalidInput
	}
	if req.Mobile != nil {
		if strings.TrimSpace(*req.Mobile) == "" {
			req.Mobile = nil
		} else if m, ok := phone.Normalize(*req.Mobile); ok {
			req.Mobile = &m
		} else {
			return req, ErrInvalidMobile
		}
	}
	return req, nil
}

func (s *Service) ListOwners(ctx context.Context, actor ActorContext, input ListOwnersRequest) ([]VehicleOwner, Pagination, error) {
	if !isAdmin(actor) {
		return nil, Pagination{}, ErrForbidden
	}
	input.Page, input.PerPage = pageOrDefault(input.Page, input.PerPage)
	owners, total, err := s.repository.ListOwners(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return owners, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) GetOwner(ctx context.Context, actor ActorContext, id int64) (VehicleOwner, error) {
	if !isAdmin(actor) {
		return VehicleOwner{}, ErrForbidden
	}
	return s.repository.FindOwner(ctx, id)
}

func (s *Service) SaveOwner(ctx context.Context, actor ActorContext, id int64, req OwnerRequest) (VehicleOwner, error) {
	if !isAdmin(actor) {
		return VehicleOwner{}, ErrForbidden
	}
	req, err := normalizeOwner(req)
	if err != nil {
		return VehicleOwner{}, err
	}
	var old *VehicleOwner
	if id > 0 {
		o, err := s.repository.FindOwner(ctx, id)
		if err != nil {
			return VehicleOwner{}, err
		}
		old = &o
	}
	owner, err := s.repository.SaveOwner(ctx, actor.UserID, id, req)
	if err != nil {
		return VehicleOwner{}, err
	}
	action := auditlog.ActionCreate
	if old != nil {
		action = auditlog.ActionUpdate
	}
	s.auditEntity(ctx, actor, auditEntityOwner, owner.ID, action, "Lorry owner "+owner.Name+" saved", old, req)
	return owner, nil
}

// SetApproval records GreenRoot's decision on a lorry and tells its drivers.
func (s *Service) SetApproval(ctx context.Context, actor ActorContext, vehicleID int64, req ApprovalRequest) (Vehicle, error) {
	if !isAdmin(actor) {
		return Vehicle{}, ErrForbidden
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	switch status {
	case "PENDING", "APPROVED", "REJECTED", "SUSPENDED":
	default:
		return Vehicle{}, ErrInvalidInput
	}
	if (status == "REJECTED" || status == "SUSPENDED") && (req.Note == nil || strings.TrimSpace(*req.Note) == "") {
		return Vehicle{}, ErrInvalidInput // drivers are told why
	}
	old, err := s.repository.FindByID(ctx, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}
	vehicle, drivers, err := s.repository.SetApproval(ctx, actor.UserID, vehicleID, status, req.Note)
	if err != nil {
		return Vehicle{}, err
	}
	s.auditEntity(ctx, actor, auditlog.EntityVehicle, vehicleID, auditlog.ActionUpdate,
		fmt.Sprintf("Vehicle %s approval %s → %s", vehicle.VehicleNumber, old.ApprovalStatus, status),
		map[string]any{"approval_status": old.ApprovalStatus}, map[string]any{"approval_status": status, "note": req.Note})
	for _, uid := range drivers {
		s.notify(ctx, uid, notifyqueue.EventVehicleDecided, "Lorry "+vehicle.VehicleNumber,
			fmt.Sprintf("GreenRoot marked lorry %s as %s.%s", vehicle.VehicleNumber, strings.ToLower(status), noteSuffix(req.Note)))
	}
	return enrichVehicle(actor, *vehicle), nil
}

func (s *Service) ListLinks(ctx context.Context, actor ActorContext, input ListLinksRequest) ([]VehicleDriverLink, Pagination, error) {
	if !isAdmin(actor) {
		return nil, Pagination{}, ErrForbidden
	}
	input.Page, input.PerPage = pageOrDefault(input.Page, input.PerPage)
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.Search = strings.TrimSpace(input.Search)
	links, total, err := s.repository.ListLinks(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return links, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

// SetLinkStatus approves, rejects, suspends or removes a driver's lorry link.
// Removal waits until the driver's trip with that lorry is over.
func (s *Service) SetLinkStatus(ctx context.Context, actor ActorContext, linkID int64, req LinkStatusRequest) (VehicleDriverLink, error) {
	if !isAdmin(actor) {
		return VehicleDriverLink{}, ErrForbidden
	}
	to := strings.ToUpper(strings.TrimSpace(req.Status))
	if to != "APPROVED" && (req.Note == nil || strings.TrimSpace(*req.Note) == "") {
		return VehicleDriverLink{}, ErrInvalidInput // the driver is told why
	}
	link, err := s.repository.FindLink(ctx, linkID)
	if err != nil {
		return VehicleDriverLink{}, err
	}
	if !linkTransitionAllowed(link.Status, to) {
		return VehicleDriverLink{}, ErrInvalidStatus
	}
	updated, err := s.repository.SetLinkStatus(ctx, actor.UserID, linkID, link.Status, to, req.Note)
	if err != nil {
		return VehicleDriverLink{}, err
	}
	s.auditEntity(ctx, actor, auditEntityVehLink, linkID, auditlog.ActionUpdate,
		fmt.Sprintf("Driver link to %s %s → %s", link.VehicleNumber, link.Status, to),
		map[string]any{"status": link.Status}, map[string]any{"status": to, "note": req.Note})
	s.notify(ctx, link.DriverUserID, notifyqueue.EventVehicleLinkDecided, "Lorry "+link.VehicleNumber,
		fmt.Sprintf("GreenRoot %s you for lorry %s.%s", linkVerb(to), link.VehicleNumber, noteSuffix(req.Note)))
	return updated, nil
}

func linkVerb(status string) string {
	switch status {
	case "APPROVED":
		return "approved"
	case "REJECTED":
		return "did not approve"
	case "SUSPENDED":
		return "suspended"
	default:
		return "removed"
	}
}

func noteSuffix(note *string) string {
	if note == nil || strings.TrimSpace(*note) == "" {
		return ""
	}
	return " " + strings.TrimSpace(*note)
}

func (s *Service) notify(ctx context.Context, userID int64, eventType, title, message string) {
	_ = notifyqueue.EnqueueOrStore(ctx, s.redis, s.db, notifyqueue.Event{
		UserID: userID, Type: eventType, Title: title, Message: message,
		Data: map[string]any{"entity": "driver_vehicles"},
	})
}

func (s *Service) auditEntity(ctx context.Context, actor ActorContext, entity string, id int64, action auditlog.Action, description string, oldValue, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID: actor.UserID, Module: auditlog.ModuleVehicles, EntityType: entity, EntityID: id,
		Action: action, Description: description, OldValue: oldValue, NewValue: newValue,
		IPAddress: actor.IPAddress, DeviceInfo: actor.UserAgent,
	})
}

func pageOrDefault(page, perPage int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 20
	}
	return page, min(perPage, 100)
}

// ─── repository ──────────────────────────────────────────────────────────────

const ownerSelect = `
	SELECT o.vehicle_owner_id, o.name, o.mobile, o.user_id, o.notes, o.created_at,
		(SELECT COUNT(*) FROM public.vehicles v WHERE v.owner_id = o.vehicle_owner_id AND v.status <> 'RETIRED')
	FROM public.vehicle_owners o`

func scanOwner(row interface{ Scan(...any) error }) (VehicleOwner, error) {
	var o VehicleOwner
	var mobile, notes sql.NullString
	var userID sql.NullInt64
	if err := row.Scan(&o.ID, &o.Name, &mobile, &userID, &notes, &o.CreatedAt, &o.VehicleCount); err != nil {
		return VehicleOwner{}, err
	}
	o.Mobile, o.Notes, o.UserID = nullableString(mobile), nullableString(notes), nullableInt64(userID)
	o.HasAccount = userID.Valid
	return o, nil
}

func (r *PostgresRepository) ListOwners(ctx context.Context, input ListOwnersRequest) ([]VehicleOwner, int64, error) {
	where, args := "", []any{}
	if s := strings.TrimSpace(input.Search); s != "" {
		args = append(args, "%"+s+"%")
		where = " WHERE o.name ILIKE $1 OR o.mobile ILIKE $1"
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.vehicle_owners o"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, input.PerPage, (input.Page-1)*input.PerPage)
	// #nosec G202 -- where is a fixed fragment; values are bound.
	rows, err := r.db.QueryContext(ctx, ownerSelect+where+
		fmt.Sprintf(" ORDER BY o.name, o.vehicle_owner_id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]VehicleOwner, 0)
	for rows.Next() {
		o, err := scanOwner(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	return out, total, rows.Err()
}

func (r *PostgresRepository) FindOwner(ctx context.Context, id int64) (VehicleOwner, error) {
	o, err := scanOwner(r.db.QueryRowContext(ctx, ownerSelect+" WHERE o.vehicle_owner_id = $1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return VehicleOwner{}, ErrNotFound
	}
	return o, err
}

// SaveOwner creates (id 0) or updates an owner. A mobile that belongs to a
// GreenRoot user links the owner to that account.
func (r *PostgresRepository) SaveOwner(ctx context.Context, actorID, id int64, req OwnerRequest) (VehicleOwner, error) {
	var err error
	if id == 0 {
		err = r.db.QueryRowContext(ctx, `
			INSERT INTO public.vehicle_owners (name, mobile, notes, user_id, created_by_user_id)
			VALUES ($1, $2::varchar, $3, (SELECT user_id FROM public.users WHERE mobile = $2::varchar LIMIT 1), $4)
			RETURNING vehicle_owner_id`, req.Name, req.Mobile, req.Notes, actorID).Scan(&id)
	} else {
		var res sql.Result
		res, err = r.db.ExecContext(ctx, `
			UPDATE public.vehicle_owners
			SET name = $2, mobile = $3::varchar, notes = $4, updated_at = CURRENT_TIMESTAMP,
			    user_id = COALESCE(user_id, (SELECT user_id FROM public.users WHERE mobile = $3::varchar LIMIT 1))
			WHERE vehicle_owner_id = $1`, id, req.Name, req.Mobile, req.Notes)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return VehicleOwner{}, ErrNotFound
			}
		}
	}
	if isUniqueViolation(err) {
		return VehicleOwner{}, ErrDuplicateOwner
	}
	if err != nil {
		return VehicleOwner{}, err
	}
	return r.FindOwner(ctx, id)
}

func (r *PostgresRepository) SetApproval(ctx context.Context, actorID, vehicleID int64, status string, note *string) (*Vehicle, []int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE public.vehicles
		SET approval_status = $2::varchar, approval_note = $3, approved_by_user_id = $4,
		    approved_at = CASE WHEN $2::varchar = 'APPROVED' THEN CURRENT_TIMESTAMP ELSE approved_at END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE vehicle_id = $1`, vehicleID, status, note, actorID)
	if err != nil {
		return nil, nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil, ErrNotFound
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT driver_user_id FROM public.vehicle_drivers
		WHERE vehicle_id = $1 AND status IN ('PENDING', 'APPROVED', 'SUSPENDED')`, vehicleID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var drivers []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		drivers = append(drivers, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	v, err := r.FindByID(ctx, vehicleID)
	return v, drivers, err
}

const linkSelect = `
	SELECT vd.vehicle_driver_id, v.vehicle_id, v.vehicle_number, v.vehicle_type, v.approval_status, o.name,
		vd.driver_user_id, NULLIF(btrim(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.mobile, d.approval_status,
		d.license_expiry_date, vd.status, vd.decision_note, vd.decided_at,
		EXISTS (SELECT 1 FROM public.dispatches t WHERE t.vehicle_id = vd.vehicle_id AND t.driver_user_id = vd.driver_user_id
			AND t.status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')),
		vd.created_at
	FROM public.vehicle_drivers vd
	JOIN public.vehicles v ON v.vehicle_id = vd.vehicle_id
	LEFT JOIN public.vehicle_owners o ON o.vehicle_owner_id = v.owner_id
	LEFT JOIN public.users u ON u.user_id = vd.driver_user_id
	LEFT JOIN LATERAL (SELECT approval_status, license_expiry_date FROM public.drivers
		WHERE user_id = vd.driver_user_id ORDER BY driver_id DESC LIMIT 1) d ON true`

func scanLink(row interface{ Scan(...any) error }) (VehicleDriverLink, error) {
	var l VehicleDriverLink
	var vehicleType, owner, name, mobile, driverApproval, note sql.NullString
	var expiry, decided sql.NullTime
	if err := row.Scan(&l.ID, &l.VehicleID, &l.VehicleNumber, &vehicleType, &l.VehicleApproval, &owner,
		&l.DriverUserID, &name, &mobile, &driverApproval, &expiry, &l.Status, &note, &decided,
		&l.IsOnActiveTrip, &l.CreatedAt); err != nil {
		return VehicleDriverLink{}, err
	}
	l.VehicleType, l.OwnerName, l.DriverName, l.DriverMobile = nullableString(vehicleType), nullableString(owner), nullableString(name), nullableString(mobile)
	l.DriverApproval, l.DecisionNote = nullableString(driverApproval), nullableString(note)
	if expiry.Valid {
		l.LicenceExpiry = &expiry.Time
	}
	if decided.Valid {
		l.DecidedAt = &decided.Time
	}
	return l, nil
}

func (r *PostgresRepository) ListLinks(ctx context.Context, input ListLinksRequest) ([]VehicleDriverLink, int64, error) {
	clauses, args := []string{"true"}, []any{}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("vd.status = ANY(string_to_array($%d, ','))", len(args)))
	}
	if input.VehicleID > 0 {
		args = append(args, input.VehicleID)
		clauses = append(clauses, fmt.Sprintf("vd.vehicle_id = $%d", len(args)))
	}
	if input.DriverUserID > 0 {
		args = append(args, input.DriverUserID)
		clauses = append(clauses, fmt.Sprintf("vd.driver_user_id = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf("(v.vehicle_number ILIKE $%d OR u.mobile ILIKE $%d OR u.first_name ILIKE $%d OR u.last_name ILIKE $%d OR o.name ILIKE $%d)", n, n, n, n, n))
	}
	where := " WHERE " + strings.Join(clauses, " AND ")
	var total int64
	// #nosec G202 -- clauses are fixed fragments with $n placeholders.
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM public.vehicle_drivers vd
		JOIN public.vehicles v ON v.vehicle_id = vd.vehicle_id
		LEFT JOIN public.vehicle_owners o ON o.vehicle_owner_id = v.owner_id
		LEFT JOIN public.users u ON u.user_id = vd.driver_user_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, input.PerPage, (input.Page-1)*input.PerPage)
	// #nosec G202 -- as above.
	rows, err := r.db.QueryContext(ctx, linkSelect+where+fmt.Sprintf(
		" ORDER BY (vd.status = 'PENDING') DESC, vd.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]VehicleDriverLink, 0)
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}

func (r *PostgresRepository) FindLink(ctx context.Context, id int64) (VehicleDriverLink, error) {
	l, err := scanLink(r.db.QueryRowContext(ctx, linkSelect+" WHERE vd.vehicle_driver_id = $1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return VehicleDriverLink{}, ErrNotFound
	}
	return l, err
}

// SetLinkStatus moves the link from `from` (so a concurrent change loses) and
// refuses removal while that driver is on a trip with that lorry.
func (r *PostgresRepository) SetLinkStatus(ctx context.Context, actorID, id int64, from, to string, note *string) (VehicleDriverLink, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE public.vehicle_drivers vd
		SET status = $3, decision_note = $4, updated_at = CURRENT_TIMESTAMP,
		    decided_by_user_id = CASE WHEN $3 = 'REMOVED' THEN decided_by_user_id ELSE $5 END,
		    decided_at = CASE WHEN $3 = 'REMOVED' THEN decided_at ELSE CURRENT_TIMESTAMP END,
		    removed_by_user_id = CASE WHEN $3 = 'REMOVED' THEN $5 END,
		    removed_at = CASE WHEN $3 = 'REMOVED' THEN CURRENT_TIMESTAMP END
		WHERE vd.vehicle_driver_id = $1 AND vd.status = $2
		  AND NOT ($3 = 'REMOVED' AND EXISTS (
		      SELECT 1 FROM public.dispatches t WHERE t.vehicle_id = vd.vehicle_id AND t.driver_user_id = vd.driver_user_id
		        AND t.status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')))`, id, from, to, note, actorID)
	if err != nil {
		return VehicleDriverLink{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		cur, err := r.FindLink(ctx, id)
		if err != nil {
			return VehicleDriverLink{}, err
		}
		if cur.Status == from && cur.IsOnActiveTrip {
			return VehicleDriverLink{}, ErrVehicleOnTrip
		}
		return VehicleDriverLink{}, ErrInvalidStatus
	}
	return r.FindLink(ctx, id)
}

func isUniqueViolation(err error) bool { return pgerr.Constraint(err) == "uq_vehicle_owners_mobile" }
