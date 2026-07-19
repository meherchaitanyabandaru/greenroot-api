package dispatches

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisgeo"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/redis/go-redis/v9"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
	ErrInvalidStatus    = errors.New("invalid status transition")
	ErrDuplicate        = errors.New("duplicate dispatch")
	ErrActiveDispatch   = errors.New("active dispatch already exists for order")
	ErrAlreadyAccepted  = errors.New("dispatch already accepted")
	ErrActiveDriverTrip = errors.New("driver already has an active trip")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	redis      redis.Cmdable
	liveGeo    *redisgeo.Service
}

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
	enrichDispatches(actor, dispatches)
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
	return withLifecycle(actor, *dispatch), nil
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
	return withLifecycle(actor, *dispatch), nil
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
	if err := s.canAccess(ctx, actor, *current); err != nil {
		return Dispatch{}, err
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if status == "" {
		status = strings.ToUpper(strings.TrimSpace(req.StatusAlias))
	}
	if !AllowedStatus(status) {
		return Dispatch{}, ErrInvalidInput
	}
	if !CanTransition(current.Status, status) {
		return Dispatch{}, ErrInvalidStatus
	}
	deliveryDate, err := parseOptionalTime(req.DeliveryDate)
	if err != nil {
		return Dispatch{}, ErrInvalidInput
	}
	dispatch, err := s.repository.UpdateStatus(ctx, dispatchID, UpdateStatusInput{Status: status, DeliveryDate: deliveryDate, Notes: req.Notes})
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
	if status == "DISPATCHED" && dispatch.CustomerUserID != nil {
		s.enqueueNotification(ctx, *dispatch.CustomerUserID,
			notifyqueue.EventOrderDispatched,
			"Order Dispatched",
			fmt.Sprintf("Your order dispatch %s is on the way.", dispatch.DispatchCode))
	}
	if IsLiveTrackingTerminal(status) {
		s.removeLiveDriverLocation(ctx, *dispatch)
	}
	s.audit(ctx, actor, "dispatches", dispatch.ID, actionUpdate, req)
	return withLifecycle(actor, *dispatch), nil
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
	return withLifecycle(actor, *updated), nil
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
	event, err := s.repository.CreateTripEvent(ctx, CreateTripEventInput{
		DispatchID:      dispatchID,
		EventType:       strings.ToUpper(strings.TrimSpace(req.EventType)),
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

// GetByCode returns dispatch info for a trip code (no auth required — used by drivers scanning QR).
func (s *Service) GetByCode(ctx context.Context, code string) (Dispatch, error) {
	dispatch, err := s.repository.FindByCode(ctx, code)
	if err != nil {
		return Dispatch{}, err
	}
	return withLifecycle(ActorContext{}, *dispatch), nil
}

func (s *Service) AcceptDispatch(ctx context.Context, actor ActorContext, dispatchID int64) (Dispatch, error) {
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
	// If already accepted by this same driver, return the dispatch (idempotent).
	if dispatch.Status == "ACCEPTED" && dispatch.DriverUserID != nil && *dispatch.DriverUserID == actor.UserID {
		return withLifecycle(actor, *dispatch), nil
	}
	if dispatch.Status != "PENDING" {
		return Dispatch{}, ErrForbidden
	}
	if dispatch.DriverID != nil || dispatch.DriverUserID != nil {
		return Dispatch{}, ErrAlreadyAccepted
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
	if updated.OwnerUserIDSnapshot != nil {
		s.enqueueNotification(ctx, *updated.OwnerUserIDSnapshot,
			notifyqueue.EventDispatchAccepted,
			"Dispatch Accepted",
			fmt.Sprintf("Dispatch %s was accepted by the driver.", updated.DispatchCode))
	}
	return withLifecycle(actor, *updated), nil
}

func (s *Service) enqueueNotification(ctx context.Context, userID int64, notifType, title, message string) {
	if err := notifyqueue.Enqueue(ctx, s.redis, notifyqueue.Event{
		UserID:  userID,
		Type:    notifType,
		Title:   title,
		Message: message,
	}); err != nil {
		slog.Warn("notification queue enqueue failed; falling back to direct notification", "type", notifType, "user_id", userID, "error", err)
		_ = s.repository.CreateNotification(ctx, userID, notifType, title, message)
	}
}

func (s *Service) GetPublicTracking(ctx context.Context, uuid string) (Dispatch, error) {
	dispatch, err := s.repository.FindByTrackingUUID(ctx, uuid)
	if err != nil {
		return Dispatch{}, err
	}
	return withLifecycle(ActorContext{}, *dispatch), nil
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
	if req.DispatchNumber == nil || strings.TrimSpace(*req.DispatchNumber) == "" {
		number := fmt.Sprintf("GR-DSP-%d", time.Now().UnixNano())
		req.DispatchNumber = &number
	}
	return CreateDispatchInput{OrderID: req.OrderID, DispatchNumber: req.DispatchNumber, VehicleID: req.VehicleID, DriverID: req.DriverID, DispatchDate: dispatchDate, DestinationAddress: req.DestinationAddress, Notes: req.Notes, Items: req.Items}, nil
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

func enrichDispatches(actor ActorContext, dispatches []Dispatch) {
	for i := range dispatches {
		dispatches[i] = withLifecycle(actor, dispatches[i])
	}
}

func withLifecycle(actor ActorContext, dispatch Dispatch) Dispatch {
	lc := lifecycle.Dispatch(dispatch.Status)
	dispatch.Lifecycle = &lc
	caps := BuildCapabilities(actor, dispatch.Status)
	dispatch.Capabilities = &caps
	return dispatch
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
