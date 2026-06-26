package nurseries

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
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
//	@Summary	List nurseries
//	@Tags		Nurseries
//	@Success	200	{object}	NurseriesResponse
//	@Router		/api/v1/nurseries [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	nurseries, pagination, err := h.service.List(r.Context(), listNurseriesRequest(r))
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, NurseriesResponse{Nurseries: nurseries, Pagination: pagination})
}

// Get godoc
//
//	@Summary	Get nursery
//	@Tags		Nurseries
//	@Success	200	{object}	NurseryResponse
//	@Router		/api/v1/nurseries/{id} [get]
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseries, err := h.service.ListMine(r.Context(), actor.UserID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, NurseriesResponse{Nurseries: nurseries})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	nursery, err := h.service.Get(r.Context(), nurseryID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, NurseryResponse{Nursery: nursery})
}

// Create godoc
//
//	@Summary	Create nursery
//	@Tags		Nurseries
//	@Security	BearerAuth
//	@Success	201	{object}	NurseryResponse
//	@Router		/api/v1/nurseries [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateNurseryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	nursery, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, NurseryResponse{Nursery: nursery})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateNurseryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	nursery, err := h.service.Update(r.Context(), actor, nurseryID, req)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, NurseryResponse{Nursery: nursery})
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	nursery, err := h.service.UpdateStatus(r.Context(), actor, nurseryID, req.Status)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, NurseryResponse{Nursery: nursery})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, nurseryID); err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Nursery deleted successfully"})
}

func (h *Handler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	addresses, err := h.service.ListAddresses(r.Context(), actor, nurseryID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, AddressesResponse{Addresses: addresses})
}

func (h *Handler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req AddressRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	address, err := h.service.CreateAddress(r.Context(), actor, nurseryID, req)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, AddressResponse{Address: address})
}

func (h *Handler) UpdateAddress(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	addressID, ok := pathID(w, r, "addressId")
	if !ok {
		return
	}
	var req AddressRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	address, err := h.service.UpdateAddress(r.Context(), actor, addressID, req)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, AddressResponse{Address: address})
}

func (h *Handler) DeleteAddress(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	addressID, ok := pathID(w, r, "addressId")
	if !ok {
		return
	}
	if err := h.service.DeleteAddress(r.Context(), actor, addressID); err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Nursery address deleted successfully"})
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	users, err := h.service.ListUsers(r.Context(), actor, nurseryID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, UsersResponse{Users: users})
}

func (h *Handler) AddUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req AddUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := h.service.AddUser(r.Context(), actor, nurseryID, req)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, UserResponse{User: user})
}

// OwnedNursery returns the nursery owned by the authenticated user.
func (h *Handler) OwnedNursery(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nursery, err := h.service.GetOwned(r.Context(), actor.UserID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, NurseryResponse{Nursery: nursery})
}

func (h *Handler) ListManagers(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	managers, err := h.service.ListManagers(r.Context(), actor, nurseryID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, UsersResponse{Users: managers})
}

func (h *Handler) AddManager(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req AddManagerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	manager, err := h.service.AddManager(r.Context(), actor, nurseryID, req)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, UserResponse{User: manager})
}

func (h *Handler) ListDrivers(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	drivers, err := h.service.ListConnectedDrivers(r.Context(), actor, nurseryID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, DriversResponse{Drivers: drivers})
}

func (h *Handler) ConnectDriver(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req ConnectDriverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	nd, err := h.service.ConnectDriver(r.Context(), actor, nurseryID, req.DriverUserID)
	if err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"driver": nd})
}

func (h *Handler) ApproveDriver(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	driverUserID, ok := pathID(w, r, "driverUserId")
	if !ok {
		return
	}
	if err := h.service.ApproveDriverConnection(r.Context(), actor, nurseryID, driverUserID); err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Driver connection approved"})
}

func (h *Handler) RemoveUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	userID, ok := pathID(w, r, "userId")
	if !ok {
		return
	}
	if err := h.service.RemoveUser(r.Context(), actor, nurseryID, userID); err != nil {
		writeNurseriesError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Nursery user removed successfully"})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listNurseriesRequest(r *http.Request) ListNurseriesRequest {
	query := r.URL.Query()
	return ListNurseriesRequest{
		Page:               intQuery(query.Get("page")),
		PerPage:            intQuery(query.Get("per_page")),
		Search:             query.Get("search"),
		City:               query.Get("city"),
		State:              query.Get("state"),
		NurseryStatus:      query.Get("nursery_status"),
		VerificationStatus: query.Get("verification_status"),
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

func writeNurseriesError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage nursery")
	case errors.Is(err, ErrNotNurseryOwner):
		response.Error(w, http.StatusForbidden, "not_owner", "only the nursery owner can perform this action")
	case errors.Is(err, ErrAlreadyOwner):
		response.Error(w, http.StatusConflict, "already_owner", "user already owns a nursery; create a new account to own another nursery")
	case errors.Is(err, ErrManagerCannotOwnNursery):
		response.Error(w, http.StatusConflict, "manager_conflict", "managers cannot register a nursery; remove your manager role first")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "nursery resource not found")
	case errors.Is(err, ErrInvalidAddress):
		response.Error(w, http.StatusBadRequest, "invalid_address", "invalid nursery address")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid nursery input")
	default:
		response.Error(w, http.StatusInternalServerError, "nurseries_error", "nurseries request failed")
	}
}
