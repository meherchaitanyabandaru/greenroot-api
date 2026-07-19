package orders

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Handler struct {
	service *Service
	jwt     *jwtplatform.Service
}

func NewHandler(service *Service, jwt *jwtplatform.Service) *Handler {
	return &Handler{service: service, jwt: jwt}
}

// List godoc
//
//	@Summary	List orders
//	@Tags		Orders
//	@Security	BearerAuth
//	@Success	200	{object}	OrdersResponse
//	@Router		/api/v1/orders [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orders, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrdersResponse{Orders: orders, Pagination: pagination})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.Get(r.Context(), actor, orderID)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	rawActor, _ := authctx.ActorFromContext(r.Context())
	if !authctx.RequireActiveNursery(w, rawActor) || !authctx.RequireActiveSubscription(w, rawActor) {
		return
	}
	var req CreateOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, OrderResponse{Order: order})
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.UpdateStatus(r.Context(), actor, orderID, req)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

func (h *Handler) UpdateDeliverySnapshot(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req DeliverySnapshotRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.UpdateDeliverySnapshot(r.Context(), actor, orderID, req)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, orderID); err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Order deleted successfully"})
}

func (h *Handler) ListItems(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	items, err := h.service.ListItems(r.Context(), actor, orderID)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, ItemsResponse{Items: items})
}

func (h *Handler) CreateItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req OrderItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateItem(r.Context(), actor, orderID, req)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, ItemResponse{Item: item})
}

func (h *Handler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	itemID, ok := pathID(w, r, "itemId")
	if !ok {
		return
	}
	var req OrderItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.UpdateItem(r.Context(), actor, itemID, req)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, ItemResponse{Item: item})
}

func (h *Handler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	itemID, ok := pathID(w, r, "itemId")
	if !ok {
		return
	}
	if err := h.service.DeleteItem(r.Context(), actor, itemID); err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Order item deleted successfully"})
}

// ConfirmOrder transitions an order from PENDING to CONFIRMED status.
func (h *Handler) ConfirmOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.UpdateStatus(r.Context(), actor, orderID, UpdateStatusRequest{Status: "CONFIRMED"})
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

// StartLoading transitions an order to LOADING status.
func (h *Handler) StartLoading(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.StartLoading(r.Context(), actor, orderID)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

// CompleteLoading transitions an order to LOADED status.
func (h *Handler) CompleteLoading(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.CompleteLoading(r.Context(), actor, orderID)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

// CancelOrder cancels an order. Reason is optional — empty body is valid.
func (h *Handler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	// reason is optional — ignore decode errors (empty body is fine)
	_ = json.NewDecoder(r.Body).Decode(&req)
	r.Body.Close()
	order, err := h.service.Cancel(r.Context(), actor, orderID, req.Reason)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

// SetLoadedQuantity records the physically loaded quantity for an order item.
// Only allowed while order is in LOADING status.
func (h *Handler) SetLoadedQuantity(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	itemID, ok := pathID(w, r, "itemId")
	if !ok {
		return
	}
	var req SetLoadedQuantityRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.SetLoadedQuantity(r.Context(), actor, orderID, itemID, req.LoadedQuantity)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, ItemResponse{Item: item})
}

// AssignManager assigns a manager to an order.
func (h *Handler) AssignManager(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		ManagerUserID int64 `json:"manager_user_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.AssignManager(r.Context(), actor, orderID, req.ManagerUserID)
	if err != nil {
		writeOrdersError(w, err)
		return
	}
	response.OK(w, OrderResponse{Order: order})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
}

func listRequest(r *http.Request) ListOrdersRequest {
	query := r.URL.Query()
	return ListOrdersRequest{
		Page:      intQuery(query.Get("page")),
		PerPage:   intQuery(query.Get("per_page")),
		Search:    query.Get("search"),
		BuyerID:   int64Query(query.Get("buyer_user_id")),
		NurseryID: int64Query(query.Get("nursery_id")),
		Status:    query.Get("order_status"),
		SortBy:    query.Get("sort_by"),
		SortOrder: query.Get("sort_order"),
		Buying:    query.Get("buying") == "true" || query.Get("buying") == "1",
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, key), 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}

func intQuery(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func int64Query(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func writeOrdersError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access order")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "order resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid order input")
	case errors.Is(err, ErrInvalidStatus):
		response.Error(w, http.StatusConflict, "invalid_status_transition", "order is not in the correct status for this action")
	case errors.Is(err, redisutil.ErrLockBusy):
		response.Error(w, http.StatusConflict, "resource_locked", "another update is already in progress")
	default:
		response.Error(w, http.StatusInternalServerError, "orders_error", "order request failed")
	}
}
