package dispatches

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

// Which lorry a driver takes on a trip. A driver may be linked to several
// lorries (public.vehicle_drivers); the trip is taken with one the driver is
// approved for, that GreenRoot approved, that is ACTIVE (not in maintenance),
// not on another trip, of the type the trip asks for, and while the driver's
// licence is valid. public.driver_vehicle_eligibility holds the non-trip rules.

var (
	ErrNoEligibleVehicle = fmt.Errorf("%w: no lorry can take this trip", apperrs.ErrConflict)
	ErrVehicleRequired   = errors.New("choose the lorry for this trip")
	ErrVehicleNotYours   = fmt.Errorf("%w: not one of your lorries", apperrs.ErrForbidden)
	ErrVehicleOnTrip     = fmt.Errorf("%w: lorry is on another trip", apperrs.ErrConflict)
)

// VehicleBlockedError: the chosen lorry cannot take this trip, and why.
type VehicleBlockedError struct{ Reason string }

func (e VehicleBlockedError) Error() string { return "lorry cannot take this trip: " + e.Reason }
func (e VehicleBlockedError) Unwrap() error { return apperrs.ErrConflict }

// blockedMessages explain each blocked_reason to the driver.
var blockedMessages = map[string]string{
	"LINK_PENDING":          "GreenRoot has not approved you for this lorry yet.",
	"LINK_SUSPENDED":        "Your link to this lorry is suspended. Contact GreenRoot support.",
	"VEHICLE_NOT_APPROVED":  "GreenRoot has not approved this lorry yet.",
	"VEHICLE_MAINTENANCE":   "This lorry is marked under maintenance.",
	"VEHICLE_INACTIVE":      "This lorry is not active.",
	"LICENCE_EXPIRED":       "Your driving licence has expired. Update it before taking trips.",
	"VEHICLE_ON_TRIP":       "This lorry is already on another trip.",
	"VEHICLE_TYPE_MISMATCH": "This trip needs a different type of lorry.",
}

// BlockedMessage is the driver-facing text for a blocked_reason.
func BlockedMessage(reason string) string {
	if m, ok := blockedMessages[reason]; ok {
		return m
	}
	return "This lorry cannot take this trip."
}

// DriverVehicle is one of the driver's lorries as the trip preview shows it.
type DriverVehicle struct {
	VehicleID      int64    `json:"vehicle_id"`
	VehicleNumber  string   `json:"vehicle_number"`
	VehicleType    *string  `json:"vehicle_type,omitempty"`
	CapacityKG     *float64 `json:"capacity_kg,omitempty"`
	IsEligible     bool     `json:"is_eligible"`
	BlockedReason  string   `json:"blocked_reason,omitempty"`
	BlockedMessage string   `json:"blocked_message,omitempty"`
}

// forTrip adds the trip's own rule (lorry type) to each lorry's standing.
func forTrip(d Dispatch, vehicles []DriverVehicle) []DriverVehicle {
	out := make([]DriverVehicle, len(vehicles))
	for i, v := range vehicles {
		if v.BlockedReason == "" && d.RequiredVehicleType != nil &&
			(v.VehicleType == nil || !strings.EqualFold(*v.VehicleType, *d.RequiredVehicleType)) {
			v.BlockedReason = "VEHICLE_TYPE_MISMATCH"
		}
		v.IsEligible = v.BlockedReason == ""
		if !v.IsEligible {
			v.BlockedMessage = BlockedMessage(v.BlockedReason)
		}
		out[i] = v
	}
	return out
}

// pickVehicle chooses the lorry for the trip: the one asked for, or the only
// eligible one when the app did not say (apps before multi-lorry support).
func (s *Service) pickVehicle(ctx context.Context, actor ActorContext, d Dispatch, requested *int64) (int64, error) {
	all, err := s.repository.DriverVehicles(ctx, actor.UserID)
	if err != nil {
		return 0, err
	}
	vehicles := forTrip(d, all)
	if requested != nil {
		for _, v := range vehicles {
			if v.VehicleID != *requested {
				continue
			}
			switch v.BlockedReason {
			case "":
				return v.VehicleID, nil
			case "VEHICLE_TYPE_MISMATCH":
				return 0, ErrVehicleMismatch
			case "VEHICLE_ON_TRIP":
				return 0, ErrVehicleOnTrip
			default:
				return 0, VehicleBlockedError{Reason: v.BlockedReason}
			}
		}
		return 0, ErrVehicleNotYours
	}
	var eligible []int64
	mismatched := false
	for _, v := range vehicles {
		if v.IsEligible {
			eligible = append(eligible, v.VehicleID)
		}
		mismatched = mismatched || v.BlockedReason == "VEHICLE_TYPE_MISMATCH"
	}
	switch {
	case len(eligible) == 1:
		return eligible[0], nil
	case len(eligible) > 1:
		return 0, ErrVehicleRequired
	case mismatched:
		return 0, ErrVehicleMismatch
	default:
		return 0, ErrNoEligibleVehicle
	}
}

func (r *PostgresRepository) DriverVehicles(ctx context.Context, userID int64) ([]DriverVehicle, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT vehicle_id, vehicle_number, vehicle_type, capacity_kg, COALESCE(blocked_reason, '')
		FROM public.driver_vehicle_eligibility
		WHERE driver_user_id = $1
		ORDER BY blocked_reason IS NULL DESC, vehicle_number`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DriverVehicle, 0)
	for rows.Next() {
		var v DriverVehicle
		var vehicleType sql.NullString
		var capacity sql.NullFloat64
		if err := rows.Scan(&v.VehicleID, &v.VehicleNumber, &vehicleType, &capacity, &v.BlockedReason); err != nil {
			return nil, err
		}
		v.VehicleType = nullableString(vehicleType)
		if capacity.Valid {
			v.CapacityKG = &capacity.Float64
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) vehicleHasActiveTrip(ctx context.Context, vehicleID, excludeDispatchID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM public.dispatches
		WHERE vehicle_id = $1 AND dispatch_id <> $2 AND status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT'))`,
		vehicleID, excludeDispatchID).Scan(&exists)
	return exists, err
}
