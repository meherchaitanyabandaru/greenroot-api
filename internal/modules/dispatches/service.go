package dispatches

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/phone"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisgeo"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/rowversion"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden        = apperrs.ErrForbidden
	ErrInvalidInput     = apperrs.ErrInvalidInput
	ErrInvalidStatus    = errors.New("invalid status transition")
	ErrDuplicate        = errors.New("duplicate dispatch")
	ErrActiveDispatch   = errors.New("active dispatch already exists for order")
	ErrAlreadyAccepted  = errors.New("dispatch already accepted")
	ErrActiveDriverTrip = errors.New("driver already has an active trip")

	// Why a driver cannot take a trip from its QR. They wrap ErrForbidden or
	// ErrConflict, so callers checking only those still refuse.
	ErrTripTaken         = fmt.Errorf("%w: trip already taken", apperrs.ErrConflict)
	ErrTripCancelled     = fmt.Errorf("%w: trip cancelled", apperrs.ErrConflict)
	ErrTripInviteExpired = fmt.Errorf("%w: trip invite expired", apperrs.ErrConflict)
	ErrVehicleMismatch   = fmt.Errorf("%w: trip needs another lorry", apperrs.ErrConflict)
	ErrDriverNotApproved = fmt.Errorf("%w: driver not approved", apperrs.ErrForbidden)

	ErrConfirmationRequired = errors.New("delivery needs the customer's confirmation")
	ErrWrongDeliveryCode    = errors.New("wrong delivery code")
	ErrDeliveryCodeLocked   = errors.New("delivery code locked after too many wrong tries")
	ErrDeliveryCodeRequired = errors.New("delivery code required")
	ErrOverrideReason       = errors.New("override reason required")
	ErrReceiptWindowClosed  = errors.New("receipt window closed")
	ErrAlreadyConfirmed     = errors.New("delivery already confirmed")
	ErrOutsideTransport     = errors.New("trip uses an outside lorry")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	redis      redis.Cmdable
	liveGeo    *redisgeo.Service
	codeKey    []byte        // derives delivery codes; see deliveryCode
	events     EventNotifier // SMS/email events; nil = in-app only
}

// SetDeliveryCodeKey sets the server secret delivery codes are derived from.
func (s *Service) SetDeliveryCodeKey(key []byte) { s.codeKey = key }

// EventNotifier raises SMS/email events (messaging.Notifier).
type EventNotifier interface {
	NotifyAsync(ev messaging.Event)
}

// SetEvents turns on SMS/email for delivery events (nil = in-app only).
func (s *Service) SetEvents(n EventNotifier) { s.events = n }

func NewService(repository Repository, auditSvc *auditlog.Service, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	var liveGeo *redisgeo.Service
	if rdb != nil {
		liveGeo = redisgeo.New(rdb)
	}
	return &Service{repository: repository, auditSvc: auditSvc, redis: rdb, liveGeo: liveGeo}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListDispatchesRequest) ([]Dispatch, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}
	dispatches, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	for i := range dispatches {
		dispatches[i] = s.present(actor, dispatches[i])
	}
	return dispatches, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, dispatchID int64) (Dispatch, error) {
	dispatch, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	if err := s.canAccess(ctx, actor, *dispatch); err != nil {
		return Dispatch{}, err
	}
	return s.present(actor, *dispatch), nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, req CreateDispatchRequest) (Dispatch, error) {
	// Business rule: only nursery operators can create dispatches.
	// Owner accounts may still carry the legacy BUYER role after approval, so
	// authorize by positive capability instead of deny-listing role labels.
	if !actor.HasRole("NURSERY_OWNER") && !actor.HasRole("MANAGER") {
		return Dispatch{}, ErrForbidden
	}
	input, err := normalizeCreate(req)
	if err != nil {
		return Dispatch{}, err
	}
	access, err := s.repository.OrderAccess(ctx, input.OrderID)
	if err != nil {
		return Dispatch{}, err
	}
	if err := s.canAccessOrder(ctx, actor, access); err != nil {
		return Dispatch{}, err
	}
	if !IsDispatchableOrderStatus(access.OrderStatus) {
		return Dispatch{}, ErrInvalidStatus
	}
	exists, err := s.repository.HasActiveForOrder(ctx, input.OrderID)
	if err != nil {
		return Dispatch{}, err
	}
	if exists {
		return Dispatch{}, ErrActiveDispatch
	}
	if input.DispatchNumber != nil {
		duplicate, err := s.repository.HasDuplicate(ctx, *input.DispatchNumber)
		if err != nil {
			return Dispatch{}, err
		}
		if duplicate {
			return Dispatch{}, ErrDuplicate
		}
	}
	dispatch, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return Dispatch{}, err
	}
	s.audit(ctx, actor, "dispatches", dispatch.ID, actionInsert, req)
	return s.present(actor, *dispatch), nil
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, dispatchID int64, req UpdateStatusRequest) (Dispatch, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "dispatches", dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	defer lock.Release(ctx)

	current, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	staff, driver, err := s.operatorRole(ctx, actor, *current)
	if err != nil {
		return Dispatch{}, err
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if status == "" {
		status = strings.ToUpper(strings.TrimSpace(req.StatusAlias))
	}
	if !AllowedStatus(status) {
		return Dispatch{}, ErrInvalidInput
	}
	// Buyers only watch; drivers move the trip forward but cannot cancel it (no reject in V1).
	if !staff && (!driver || status == "CANCELLED") {
		return Dispatch{}, ErrForbidden
	}
	// Delivery is confirmed by the customer: POST /dispatches/{id}/deliver or /receipt.
	if status == "DELIVERED" {
		return Dispatch{}, ErrConfirmationRequired
	}
	if !CanTransition(current.Status, status) {
		return Dispatch{}, ErrInvalidStatus
	}
	deliveryDate, err := parseOptionalTime(req.DeliveryDate)
	if err != nil {
		return Dispatch{}, ErrInvalidInput
	}
	if v, ok := rowversion.Expected(ctx); ok && v != current.Version {
		return Dispatch{}, ErrConflict
	}
	previous := current.Status // read before the update (a repository may hand back the same struct)
	dispatch, err := s.repository.UpdateStatus(ctx, dispatchID, UpdateStatusInput{Status: status, DeliveryDate: deliveryDate, Notes: req.Notes, ByUserID: &actor.UserID, ExpectedVersion: current.Version})
	if err != nil {
		return Dispatch{}, err
	}
	if status == "DELIVERED" {
		if err := s.repository.CompleteOrderIfReady(ctx, dispatch.OrderID); err != nil {
			return Dispatch{}, err
		}
		if refreshed, err := s.repository.FindByID(ctx, dispatchID); err == nil {
			dispatch = refreshed
		}
	}
	// A driver may start straight from ACCEPTED: the customer still hears it left.
	if (status == "DISPATCHED" || previous == "ACCEPTED" && status == "IN_TRANSIT") && dispatch.CustomerUserID != nil {
		s.enqueueNotification(ctx, *dispatch.CustomerUserID,
			notifyqueue.EventOrderDispatched,
			"Order Dispatched",
			fmt.Sprintf("Your order dispatch %s is on the way.", dispatch.DispatchCode), notifyqueue.Link("dispatch", dispatch.ID))
		// Not on the app: the code they give the driver comes by SMS.
		if code, ok := s.deliveryCode(*dispatch); ok && s.events != nil {
			id := dispatch.ID
			s.events.NotifyAsync(messaging.Event{
				Code: sms.TemplateDeliveryOnTheWay, UserID: *dispatch.CustomerUserID, NotOnAppOnly: true,
				Values:    map[string]string{"dispatch_code": dispatch.DispatchCode, "delivery_code": code},
				Sensitive: []string{"delivery_code"}, ReferenceType: "DISPATCH", ReferenceID: &id,
			})
		}
	}
	if status == "CANCELLED" && current.DriverUserID != nil {
		s.enqueueNotification(ctx, *current.DriverUserID, "TRIP_CANCELLED", "Trip cancelled",
			fmt.Sprintf("Trip %s was cancelled by the nursery. You can take another trip.", dispatch.DispatchCode),
			notifyqueue.Link("dispatch", dispatch.ID))
	}
	if IsLiveTrackingTerminal(status) {
		s.removeLiveDriverLocation(ctx, *dispatch)
	}
	s.audit(ctx, actor, "dispatches", dispatch.ID, actionUpdate, req)
	return s.present(actor, *dispatch), nil
}

func (s *Service) AcknowledgeDeliveryUpdate(ctx context.Context, actor ActorContext, dispatchID int64) (Dispatch, error) {
	current, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	if err := s.canAccess(ctx, actor, *current); err != nil {
		return Dispatch{}, err
	}
	if err := s.repository.AcknowledgeDeliveryUpdate(ctx, dispatchID, actor.UserID); err != nil {
		return Dispatch{}, err
	}
	updated, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	s.audit(ctx, actor, "dispatches", dispatchID, actionUpdate, map[string]any{"delivery_update_acknowledged": true})
	return s.present(actor, *updated), nil
}

func (s *Service) CreateItem(ctx context.Context, actor ActorContext, dispatchID int64, req DispatchItemRequest) (DispatchItem, error) {
	dispatch, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return DispatchItem{}, err
	}
	if err := s.canAccess(ctx, actor, *dispatch); err != nil {
		return DispatchItem{}, err
	}
	if IsTerminal(dispatch.Status) {
		return DispatchItem{}, ErrInvalidStatus
	}
	if req.Quantity <= 0 {
		return DispatchItem{}, ErrInvalidInput
	}
	item, err := s.repository.CreateItem(ctx, dispatchID, req)
	if err != nil {
		return DispatchItem{}, err
	}
	s.audit(ctx, actor, "dispatch_items", item.ID, actionInsert, req)
	return *item, nil
}

// CreateTripEvent records a trip event (driver only, or admin).
func (s *Service) CreateTripEvent(ctx context.Context, actor ActorContext, dispatchID int64, req CreateTripEventRequest) (TripEvent, error) {
	dispatch, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return TripEvent{}, err
	}
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		// Must be the assigned driver
		isDriver := false
		if dispatch.DriverUserID != nil && *dispatch.DriverUserID == actor.UserID {
			isDriver = true
		}
		if dispatch.DriverID != nil {
			if ok, _ := s.repository.IsDispatchDriver(ctx, *dispatch.DriverID, actor.UserID); ok {
				isDriver = true
			}
		}
		if !isDriver {
			return TripEvent{}, ErrForbidden
		}
	}
	if IsTerminal(dispatch.Status) || dispatch.Status == "PENDING" {
		return TripEvent{}, ErrInvalidStatus
	}
	eventType := strings.ToUpper(strings.TrimSpace(req.EventType))
	if eventType == "" || len(eventType) > 50 ||
		(req.Latitude != nil && (*req.Latitude < -90 || *req.Latitude > 90)) ||
		(req.Longitude != nil && (*req.Longitude < -180 || *req.Longitude > 180)) {
		return TripEvent{}, ErrInvalidInput
	}
	event, err := s.repository.CreateTripEvent(ctx, CreateTripEventInput{
		DispatchID:      dispatchID,
		EventType:       eventType,
		Latitude:        req.Latitude,
		Longitude:       req.Longitude,
		PhotoURL:        req.PhotoURL,
		Remarks:         req.Remarks,
		CreatedByUserID: actor.UserID,
	})
	if err != nil {
		return TripEvent{}, err
	}
	s.audit(ctx, actor, "trip_events", event.ID, actionInsert, req)
	return *event, nil
}

// GetByCode lets a driver preview a trip from its QR code. Codes are
// sequential, so only a driver can open an unclaimed PENDING trip; anyone else
// needs normal access to the dispatch.
// tripRef reads a trip join code: "TRIP-<uuid>" from the nursery's QR, or the
// bare uuid. Dispatch codes and ids are sequential, so they never join a trip.
func tripRef(raw string) (string, bool) {
	v := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "trip-")
	if _, err := uuid.Parse(v); err != nil || len(v) != 36 {
		return "", false
	}
	return v, true
}

func (s *Service) GetByCode(ctx context.Context, actor ActorContext, code string) (Dispatch, error) {
	ref, ok := tripRef(code)
	if !ok {
		return Dispatch{}, ErrNotFound
	}
	dispatch, err := s.repository.FindByTripUUID(ctx, ref)
	if err != nil {
		return Dispatch{}, err
	}
	// The QR is the driver's invitation: any driver may look at an open trip
	// to decide; everyone else sees it only if they are part of it.
	if actor.HasRole("DRIVER") && !isTripDriver(actor, *dispatch) {
		if err := openForDrivers(*dispatch, time.Now()); err != nil {
			return Dispatch{}, err
		}
		return s.present(actor, *dispatch), nil
	}
	if err := s.canAccess(ctx, actor, *dispatch); err != nil {
		return Dispatch{}, err
	}
	return s.present(actor, *dispatch), nil
}

func (s *Service) AcceptDispatch(ctx context.Context, actor ActorContext, dispatchID int64, tripCode string) (Dispatch, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "dispatches", dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	defer lock.Release(ctx)

	// Only drivers may accept dispatches.
	if !actor.HasRole("DRIVER") {
		return Dispatch{}, ErrForbidden
	}
	dispatch, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	// Only someone holding the trip's QR code may join it: ids are guessable.
	if ref, ok := tripRef(tripCode); !ok || dispatch.TripUUID == nil || !strings.EqualFold(*dispatch.TripUUID, ref) {
		return Dispatch{}, ErrNotFound
	}
	// If already accepted by this same driver, return the dispatch (idempotent).
	if dispatch.Status == "ACCEPTED" && dispatch.DriverUserID != nil && *dispatch.DriverUserID == actor.UserID {
		return s.present(actor, *dispatch), nil
	}
	if err := openForDrivers(*dispatch, time.Now()); err != nil {
		return Dispatch{}, err
	}
	if err := s.requireEligibleDriver(ctx, actor, *dispatch); err != nil {
		return Dispatch{}, err
	}
	active, err := s.repository.DriverHasActiveTrip(ctx, actor.UserID, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	if active {
		return Dispatch{}, ErrActiveDriverTrip
	}
	updated, err := s.repository.SetDriverUser(ctx, dispatchID, actor.UserID)
	if err != nil {
		return Dispatch{}, err
	}
	// Sent only after the claim succeeded, so they always name the winner.
	if updated.OwnerUserIDSnapshot != nil {
		s.enqueueNotification(ctx, *updated.OwnerUserIDSnapshot,
			notifyqueue.EventDispatchAccepted,
			"Dispatch Accepted",
			fmt.Sprintf("Dispatch %s was accepted by %s.", updated.DispatchCode, textOr(updated.DriverName, "a driver")), notifyqueue.Link("dispatch", updated.ID))
	}
	s.enqueueNotification(ctx, actor.UserID,
		notifyqueue.EventDispatchAccepted,
		"Trip is yours",
		fmt.Sprintf("You have trip %s. Pick up at %s.", updated.DispatchCode, textOr(updated.SellerNurseryName, "the nursery")), notifyqueue.Link("dispatch", updated.ID))
	return s.present(actor, *updated), nil
}

// RenewInvite gives a trip still waiting for a driver a new QR (the old one
// stops working), e.g. after the 72 hours ran out. Nursery staff only.
func (s *Service) RenewInvite(ctx context.Context, actor ActorContext, dispatchID int64) (Dispatch, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "dispatches", dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	defer lock.Release(ctx)

	current, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	staff, _, err := s.operatorRole(ctx, actor, *current)
	if err != nil {
		return Dispatch{}, err
	}
	if !staff {
		return Dispatch{}, ErrForbidden
	}
	if current.IsOutsideTransport {
		return Dispatch{}, ErrOutsideTransport
	}
	updated, err := s.repository.RenewTripInvite(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	s.audit(ctx, actor, "dispatches", dispatchID, actionUpdate, map[string]any{"trip_invite": "renewed"})
	return s.present(actor, *updated), nil
}

var vehicleTypes = map[string]bool{"TRUCK": true, "VAN": true, "PICKUP": true, "AUTO": true}

func isTripDriver(actor ActorContext, d Dispatch) bool {
	return d.DriverUserID != nil && *d.DriverUserID == actor.UserID
}

// openForDrivers: the trip's QR can still be taken -- nobody has it, it was
// not cancelled, it is not an outside lorry, and the QR has not lapsed.
func openForDrivers(d Dispatch, now time.Time) error {
	switch {
	case d.IsOutsideTransport:
		return ErrOutsideTransport
	case d.Status == "CANCELLED":
		return ErrTripCancelled
	case d.Status != "PENDING" || d.DriverID != nil || d.DriverUserID != nil:
		return ErrTripTaken
	case d.InviteExpiresAt != nil && !now.Before(*d.InviteExpiresAt):
		return ErrTripInviteExpired
	}
	return nil
}

// requireEligibleDriver: any driver GreenRoot approved who is still driving,
// with the lorry the trip asks for. No link to the nursery is needed.
func (s *Service) requireEligibleDriver(ctx context.Context, actor ActorContext, d Dispatch) error {
	e, err := s.repository.DriverEligibility(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if !e.Found || e.ApprovalStatus != "APPROVED" || e.Status != "ACTIVE" {
		return ErrDriverNotApproved
	}
	if d.RequiredVehicleType != nil && !strings.EqualFold(*d.RequiredVehicleType, e.VehicleType) {
		return ErrVehicleMismatch
	}
	return nil
}

func textOr(s *string, fallback string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return fallback
	}
	return *s
}

func (s *Service) enqueueNotification(ctx context.Context, userID int64, notifType, title, message string, data map[string]any) {
	if err := notifyqueue.Enqueue(ctx, s.redis, notifyqueue.Event{
		UserID:  userID,
		Type:    notifType,
		Title:   title,
		Message: message,
		Data:    data,
	}); err != nil {
		slog.Warn("notification queue enqueue failed; falling back to direct notification", "type", notifType, "user_id", userID, "error", err)
		_ = s.repository.CreateNotification(ctx, userID, notifType, title, message, data)
	}
}

func (s *Service) GetPublicTracking(ctx context.Context, uuid string) (PublicTrip, error) {
	dispatch, err := s.repository.FindByTrackingUUID(ctx, uuid)
	if err != nil {
		return PublicTrip{}, err
	}
	return publicTrip(withLifecycle(ActorContext{}, *dispatch)), nil
}

// publicTrip copies the allow-listed fields only, so a field added to
// Dispatch later can never leak through the public link.
func publicTrip(d Dispatch) PublicTrip {
	vehicle := d.VehicleNumber
	if vehicle == nil {
		vehicle = d.OutsideVehicleNumber
	}
	t := PublicTrip{
		DispatchCode:      d.DispatchCode,
		Status:            d.Status,
		Lifecycle:         d.Lifecycle,
		SellerNurseryName: d.SellerNurseryName,
		VehicleNumber:     vehicle,
		DispatchDate:      d.DispatchDate,
		TripStartedAt:     d.TripStartedAt,
		DeliveryDate:      d.DeliveryDate,
	}
	if onTheWay(d.Status) {
		for _, e := range d.TripEvents {
			t.TripEvents = append(t.TripEvents, PublicTripEvent{
				EventType: e.EventType, Latitude: e.Latitude, Longitude: e.Longitude, CreatedAt: e.CreatedAt,
			})
		}
	}
	return t
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListDispatchesRequest) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") {
		if input.Buying {
			// Buyer perspective: incoming dispatches for orders this owner placed as buyer.
			input.BuyerUserID = actor.UserID
			if actor.HasRole("NURSERY_OWNER") {
				nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
				if nurseryID != nil {
					input.BuyerNurseryID = *nurseryID
				}
			}
			return nil
		}
		if input.OrderID > 0 {
			access, err := s.repository.OrderAccess(ctx, input.OrderID)
			if err != nil {
				return err
			}
			return s.canAccessOrder(ctx, actor, access)
		}
		if input.NurseryID > 0 {
			// Explicit nursery_id: verify the user is a member/owner of that nursery.
			member, err := s.repository.IsNurseryMember(ctx, input.NurseryID, actor.UserID)
			if err != nil {
				return err
			}
			if !member {
				return ErrForbidden
			}
			return nil
		}
		// No nursery_id given: auto-scope to all nurseries this user owns/manages.
		nurseryIDs, err := s.repository.GetUserNurseryIDs(ctx, actor.UserID)
		if err != nil {
			return err
		}
		if len(nurseryIDs) == 0 {
			return ErrForbidden
		}
		// Use first nursery; multi-nursery support can extend this later.
		input.NurseryID = nurseryIDs[0]
		return nil
	}
	if actor.HasRole("DRIVER") {
		input.DriverUserID = actor.UserID
		return nil
	}
	if actor.HasRole("BUYER") {
		// Buyers see dispatches for their own orders only.
		input.BuyerUserID = actor.UserID
		return nil
	}
	return ErrForbidden
}

// operatorRole reports whether the actor runs this dispatch as seller staff
// (admin, owner or manager of the selling nursery) or as its assigned driver.
func (s *Service) operatorRole(ctx context.Context, actor ActorContext, dispatch Dispatch) (staff, driver bool, err error) {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return true, false, nil
	}
	if dispatch.SellerNurseryID != nil && (actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER")) {
		member, err := s.repository.IsNurseryMember(ctx, *dispatch.SellerNurseryID, actor.UserID)
		if err != nil {
			return false, false, err
		}
		if member {
			return true, false, nil
		}
	}
	if actor.HasRole("DRIVER") {
		if dispatch.DriverUserID != nil && *dispatch.DriverUserID == actor.UserID {
			return false, true, nil
		}
		if dispatch.DriverID != nil {
			if ok, err := s.repository.IsDispatchDriver(ctx, *dispatch.DriverID, actor.UserID); err == nil && ok {
				return false, true, nil
			}
		}
	}
	return false, false, nil
}

func (s *Service) canAccess(ctx context.Context, actor ActorContext, dispatch Dispatch) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if dispatch.SellerNurseryID != nil {
		// Both owners and managers of the seller nursery can manage the dispatch.
		if actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") {
			member, err := s.repository.IsNurseryMember(ctx, *dispatch.SellerNurseryID, actor.UserID)
			if err != nil {
				return err
			}
			if member {
				return nil
			}
		}
	}
	if actor.HasRole("DRIVER") {
		if dispatch.DriverUserID != nil && *dispatch.DriverUserID == actor.UserID {
			return nil
		}
		if dispatch.DriverID != nil {
			isDriver, err := s.repository.IsDispatchDriver(ctx, *dispatch.DriverID, actor.UserID)
			if err == nil && isDriver {
				return nil
			}
		}
	}
	if actor.HasRole("BUYER") {
		// Buyer can access dispatch if it belongs to their order.
		access, err := s.repository.OrderAccess(ctx, dispatch.OrderID)
		if err == nil && access.BuyerID != nil && *access.BuyerID == actor.UserID {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) canAccessOrder(ctx context.Context, actor ActorContext, access *OrderAccess) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if (actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER")) && access.NurseryID != nil {
		member, err := s.repository.IsNurseryMember(ctx, *access.NurseryID, actor.UserID)
		if err != nil {
			return err
		}
		if member {
			return nil
		}
	}
	if actor.HasRole("BUYER") && access.BuyerID != nil && *access.BuyerID == actor.UserID {
		return nil
	}
	return ErrForbidden
}

func normalizeCreate(req CreateDispatchRequest) (CreateDispatchInput, error) {
	if req.OrderID <= 0 {
		return CreateDispatchInput{}, ErrInvalidInput
	}
	dispatchDate, err := parseOptionalTime(req.DispatchDate)
	if err != nil {
		return CreateDispatchInput{}, ErrInvalidInput
	}
	for _, item := range req.Items {
		if item.Quantity <= 0 {
			return CreateDispatchInput{}, ErrInvalidInput
		}
	}
	input := CreateDispatchInput{OrderID: req.OrderID, DispatchNumber: req.DispatchNumber, VehicleID: req.VehicleID, DispatchDate: dispatchDate, DestinationAddress: req.DestinationAddress, Notes: req.Notes, Items: req.Items}
	if req.IsOutsideTransport {
		vehicle := strings.ToUpper(strings.TrimSpace(stringOrEmpty(req.OutsideVehicleNumber)))
		name := strings.TrimSpace(stringOrEmpty(req.OutsideDriverName))
		if vehicle == "" || len(vehicle) > 20 || len([]rune(name)) > 100 {
			return CreateDispatchInput{}, ErrInvalidInput
		}
		input.IsOutsideTransport = true
		input.OutsideVehicleNumber, input.OutsideDriverName = &vehicle, &name
		if raw := strings.TrimSpace(stringOrEmpty(req.OutsideDriverMobile)); raw != "" {
			mobile, ok := phone.Normalize(raw)
			if !ok {
				return CreateDispatchInput{}, ErrInvalidInput
			}
			input.OutsideDriverMobile = &mobile
		}
	}
	if v := strings.ToUpper(strings.TrimSpace(stringOrEmpty(req.RequiredVehicleType))); v != "" && !input.IsOutsideTransport {
		if !vehicleTypes[v] {
			return CreateDispatchInput{}, ErrInvalidInput
		}
		input.RequiredVehicleType = &v
	}
	// Empty dispatch number: the repository uses the public code (DSP-YYYYMMDD-NNNN).
	return input, nil
}

func normalizeList(input ListDispatchesRequest) ListDispatchesRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.Search = strings.TrimSpace(input.Search)
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = "desc"
	}
	return input
}

func parseOptionalTime(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	text := strings.TrimSpace(*value)
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return &parsed, nil
	}
	parsed, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// Status predicates and CanTransition are defined in policy.go.

func (s *Service) removeLiveDriverLocation(ctx context.Context, dispatch Dispatch) {
	if s.liveGeo == nil {
		return
	}
	driverID := int64(0)
	if dispatch.DriverUserID != nil && *dispatch.DriverUserID > 0 {
		driverID = *dispatch.DriverUserID
	} else if dispatch.DriverID != nil && *dispatch.DriverID > 0 {
		driverID = *dispatch.DriverID
	}
	if driverID <= 0 {
		return
	}
	if err := s.liveGeo.RemoveDriver(ctx, driverID); err != nil {
		slog.Warn("redis geo live driver cleanup skipped", "dispatch_id", dispatch.ID, "driver_id", driverID, "error", err)
	}
}

func totalPages(total int64, perPage int) int {
	if perPage <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(perPage)))
}

func withLifecycle(actor ActorContext, dispatch Dispatch) Dispatch {
	lc := lifecycle.Dispatch(dispatch.Status)
	dispatch.Lifecycle = &lc
	buyerSide := isCustomer(actor, dispatch)
	caps := BuildCapabilities(actor, dispatch.Status, buyerSide)
	if dispatch.IsOutsideTransport { // no app driver: nothing to accept, share or start
		caps.CanAccept, caps.CanShareCode, caps.CanStartTrip = false, false, false
	} else {
		// An app driver starts the trip themselves; the hand-over button only confused owners.
		caps.CanMarkDispatched = false
		caps.CanRenewInvite = caps.CanShareCode && dispatch.DriverUserID == nil
		if dispatch.InviteExpiresAt != nil && !time.Now().Before(*dispatch.InviteExpiresAt) {
			caps.CanAccept, caps.CanShareCode = false, false // the QR lapsed: only a new one helps
		}
	}
	caps.CanConfirmReceipt, caps.CanReportIssue = receiptActions(dispatch, buyerSide, time.Now())
	dispatch.Capabilities = &caps
	return dispatch
}

// present is what an actor receives: lifecycle, capabilities and, for the
// customer only, the delivery code while the trip is on its way.
func (s *Service) present(actor ActorContext, dispatch Dispatch) Dispatch {
	dispatch = withLifecycle(actor, dispatch)
	// A driver deciding on a trip sees where it goes, not who the customer
	// is: anyone with the QR may look, and only the one who takes it calls.
	if actor.HasRole("DRIVER") && !isTripDriver(actor, dispatch) && !isCustomer(actor, dispatch) {
		dispatch.CustomerNameSnapshot, dispatch.CustomerMobileSnapshot, dispatch.CustomerUserID = nil, nil, nil
	}
	if isCustomer(actor, dispatch) && onTheWay(dispatch.Status) && dispatch.DeliveryCodeAttempts < maxDeliveryCodeAttempts {
		if code, ok := s.deliveryCode(dispatch); ok {
			dispatch.DeliveryCode = &code
		}
	}
	return dispatch
}

func isCustomer(actor ActorContext, d Dispatch) bool {
	return actor.UserID != 0 && d.CustomerUserID != nil && *d.CustomerUserID == actor.UserID
}

func onTheWay(status string) bool { return status == "DISPATCHED" || status == "IN_TRANSIT" }

// ── Delivery confirmation ─────────────────────────────────────────────────────

const (
	maxDeliveryCodeAttempts = 5
	receiptWindow           = 48 * time.Hour
	minOverrideReason       = 10
	maxIssueLength          = 500
)

// deliveryCode is the trip's 4-digit code, derived (HMAC) from a server
// secret and the trip id, so it is never stored and the same on every read.
// With 5 tries per trip a guess succeeds 1 time in 2,000.
func (s *Service) deliveryCode(d Dispatch) (string, bool) {
	if len(s.codeKey) == 0 || d.TripUUID == nil {
		return "", false
	}
	mac := hmac.New(sha256.New, s.codeKey)
	mac.Write([]byte("greenroot/delivery-code/v1/" + *d.TripUUID))
	return fmt.Sprintf("%04d", binary.BigEndian.Uint32(mac.Sum(nil)[:4])%10000), true
}

// receiptActions: while on the way the customer can confirm receipt; for 48
// hours after delivery they can confirm an unconfirmed one or report a problem.
func receiptActions(d Dispatch, buyerSide bool, now time.Time) (confirm, report bool) {
	if !buyerSide {
		return false, false
	}
	if onTheWay(d.Status) {
		return true, true
	}
	if d.Status != "DELIVERED" || d.DeliveryDate == nil || now.Sub(*d.DeliveryDate) > receiptWindow {
		return false, false
	}
	return d.DeliveryConfirmation != nil && *d.DeliveryConfirmation == ConfirmationUnconfirmed, d.DeliveryIssue == nil
}

// Deliver marks the trip delivered by the driver or nursery: with the
// customer's code, or (nursery staff only) without it, giving a reason the
// customer is told about and can dispute.
func (s *Service) Deliver(ctx context.Context, actor ActorContext, dispatchID int64, req DeliverRequest) (Dispatch, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "dispatches", dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	defer lock.Release(ctx)

	current, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	staff, driver, err := s.operatorRole(ctx, actor, *current)
	if err != nil {
		return Dispatch{}, err
	}
	if (!staff && !driver) || isCustomer(actor, *current) {
		return Dispatch{}, ErrForbidden
	}
	if !onTheWay(current.Status) {
		return Dispatch{}, ErrInvalidStatus
	}

	confirmation, reason := ConfirmationCode, (*string)(nil)
	if code := strings.TrimSpace(req.Code); code != "" {
		want, ok := s.deliveryCode(*current)
		if !ok {
			return Dispatch{}, ErrDeliveryCodeLocked
		}
		// Every try spends one of the trip's attempts in a single UPDATE
		// before the code is compared, so parallel guesses cannot exceed the
		// limit even if the Redis lock above is unavailable.
		attempts, allowed, err := s.repository.UseDeliveryCodeAttempt(ctx, dispatchID, maxDeliveryCodeAttempts)
		if err != nil {
			return Dispatch{}, err
		}
		if !allowed {
			return Dispatch{}, ErrDeliveryCodeLocked
		}
		if !hmac.Equal([]byte(code), []byte(want)) {
			s.audit(ctx, actor, "dispatches", dispatchID, actionUpdate, map[string]any{"wrong_delivery_code": attempts})
			if attempts >= maxDeliveryCodeAttempts {
				return Dispatch{}, ErrDeliveryCodeLocked
			}
			return Dispatch{}, ErrWrongDeliveryCode
		}
	} else {
		r := strings.TrimSpace(stringOrEmpty(req.OverrideReason))
		if !staff {
			return Dispatch{}, ErrDeliveryCodeRequired // drivers always need the code
		}
		if len([]rune(r)) < minOverrideReason || len([]rune(r)) > maxIssueLength {
			return Dispatch{}, ErrOverrideReason
		}
		confirmation, reason = ConfirmationUnconfirmed, &r
	}

	dispatch, err := s.markDelivered(ctx, actor, *current, confirmation, reason, nil)
	if err != nil {
		return Dispatch{}, err
	}
	if dispatch.CustomerUserID != nil {
		if confirmation == ConfirmationUnconfirmed {
			s.enqueueNotification(ctx, *dispatch.CustomerUserID, notifyqueue.EventOrderDelivered,
				"Delivery marked without your confirmation",
				fmt.Sprintf("%s was marked delivered by the nursery: %s. If you did not receive your plants, report a problem in the app within 48 hours.", dispatch.DispatchCode, *reason), notifyqueue.Link("dispatch", dispatch.ID))
			// The dispute window only protects a customer who hears about it:
			// also SMS and email, not just the in-app inbox and push.
			if s.events != nil {
				id := dispatch.ID
				s.events.NotifyAsync(messaging.Event{
					Code: sms.TemplateDeliveryUnconfirmed, UserID: *dispatch.CustomerUserID,
					Values:        map[string]string{"dispatch_code": dispatch.DispatchCode, "reason": *reason},
					ReferenceType: "DISPATCH", ReferenceID: &id,
				})
			}
		} else {
			s.enqueueNotification(ctx, *dispatch.CustomerUserID, notifyqueue.EventOrderDelivered,
				"Order delivered", fmt.Sprintf("%s was delivered with your code.", dispatch.DispatchCode), notifyqueue.Link("dispatch", dispatch.ID))
		}
	}
	s.audit(ctx, actor, "dispatches", dispatch.ID, actionUpdate, map[string]any{"delivered": confirmation, "override_reason": reason})
	return s.present(actor, dispatch), nil
}

// ConfirmReceipt is the customer's side: "I received the plants" while the
// trip is on its way (this delivers it), or within 48 hours of delivery to
// confirm a nursery-marked delivery and/or report a problem.
func (s *Service) ConfirmReceipt(ctx context.Context, actor ActorContext, dispatchID int64, req ReceiptRequest) (Dispatch, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "dispatches", dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	defer lock.Release(ctx)

	current, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	if !isCustomer(actor, *current) {
		return Dispatch{}, ErrForbidden
	}
	issue := strings.TrimSpace(stringOrEmpty(req.Issue))
	if len([]rune(issue)) > maxIssueLength {
		return Dispatch{}, ErrInvalidInput
	}
	var issuePtr *string
	if issue != "" {
		issuePtr = &issue
	}

	var dispatch Dispatch
	switch {
	case onTheWay(current.Status):
		dispatch, err = s.markDelivered(ctx, actor, *current, ConfirmationApp, nil, issuePtr)
	case current.Status == "DELIVERED":
		if current.DeliveryDate == nil || time.Since(*current.DeliveryDate) > receiptWindow {
			return Dispatch{}, ErrReceiptWindowClosed
		}
		canConfirm, canReport := receiptActions(*current, true, time.Now())
		if !canConfirm && (issuePtr == nil || !canReport) {
			return Dispatch{}, ErrAlreadyConfirmed
		}
		if !canReport {
			issuePtr = nil
		}
		if err = s.repository.RecordReceipt(ctx, dispatchID, actor.UserID, canConfirm, issuePtr); err == nil {
			var refreshed *Dispatch
			if refreshed, err = s.repository.FindByID(ctx, dispatchID); err == nil {
				dispatch = *refreshed
			}
		}
	default:
		return Dispatch{}, ErrInvalidStatus
	}
	if err != nil {
		return Dispatch{}, err
	}
	if issuePtr != nil && dispatch.OwnerUserIDSnapshot != nil {
		s.enqueueNotification(ctx, *dispatch.OwnerUserIDSnapshot, notifyqueue.EventDeliveryIssue,
			"Customer reported a delivery problem",
			fmt.Sprintf("%s: %s", dispatch.DispatchCode, issue), notifyqueue.Link("dispatch", dispatch.ID))
		if s.events != nil {
			id := dispatch.ID
			s.events.NotifyAsync(messaging.Event{Code: sms.TemplateDeliveryIssue, UserID: *dispatch.OwnerUserIDSnapshot,
				Values: map[string]string{"dispatch_code": dispatch.DispatchCode}, ReferenceType: "DISPATCH", ReferenceID: &id})
		}
	}
	s.audit(ctx, actor, "dispatches", dispatchID, actionUpdate, map[string]any{"receipt_confirmed": true, "issue": issuePtr})
	return s.present(actor, dispatch), nil
}

func (s *Service) markDelivered(ctx context.Context, actor ActorContext, current Dispatch, confirmation string, reason, issue *string) (Dispatch, error) {
	updated, err := s.repository.MarkDelivered(ctx, current.ID, MarkDeliveredInput{
		Confirmation: confirmation, ByUserID: actor.UserID, OverrideReason: reason, Issue: issue,
	})
	if err != nil {
		return Dispatch{}, err
	}
	if err := s.repository.CompleteOrderIfReady(ctx, updated.OrderID); err != nil {
		return Dispatch{}, err
	}
	if refreshed, err := s.repository.FindByID(ctx, current.ID); err == nil {
		updated = refreshed
	}
	s.removeLiveDriverLocation(ctx, *updated)
	if updated.OwnerUserIDSnapshot != nil && *updated.OwnerUserIDSnapshot != actor.UserID {
		s.enqueueNotification(ctx, *updated.OwnerUserIDSnapshot, notifyqueue.EventOrderDelivered,
			"Order delivered", fmt.Sprintf("%s for order %s was delivered.", updated.DispatchCode, stringOrEmpty(updated.OrderNumber)),
			notifyqueue.Link("dispatch", updated.ID))
	}
	return *updated, nil
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleDispatches,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}
