package sourcing

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

// ---- Network membership ----

// GetMembership godoc
//
//	@Summary	Get nursery sourcing network membership
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	200	{object}	MemberResponse
//	@Router		/api/v1/nurseries/{nurseryId}/sourcing-membership [get]
func (h *Handler) GetMembership(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	m, err := h.service.GetMembership(r.Context(), actor, nurseryID)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, MemberResponse{Member: m})
}

// JoinNetwork godoc
//
//	@Summary	Join the Plant Sourcing Network
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	200	{object}	MemberResponse
//	@Router		/api/v1/nurseries/{nurseryId}/sourcing-membership [post]
func (h *Handler) JoinNetwork(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	var req JoinNetworkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := h.service.JoinNetwork(r.Context(), actor, nurseryID, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, MemberResponse{Member: m})
}

// LeaveNetwork godoc
//
//	@Summary	Leave the Plant Sourcing Network
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Router		/api/v1/nurseries/{nurseryId}/sourcing-membership [delete]
func (h *Handler) LeaveNetwork(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	if err := h.service.LeaveNetwork(r.Context(), actor, nurseryID); err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, map[string]string{"message": "left sourcing network"})
}

// ---- Discovery ----

// ListNearby godoc
//
//	@Summary	Discover nearby nurseries in the Plant Sourcing Network
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Success	200	{object}	NearbyNurseriesResponse
//	@Router		/api/v1/sourcing-network/nurseries [get]
func (h *Handler) ListNearby(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := nearbyQuery(r)
	nurseries, pg, err := h.service.ListNearby(r.Context(), actor, q)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, NearbyNurseriesResponse{Nurseries: nurseries, Pagination: pg})
}

// GetNurseryProfile godoc
//
//	@Summary	Get a network nursery's sourcing profile
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	200	{object}	NearbyNursery
//	@Router		/api/v1/sourcing-network/nurseries/{nurseryId} [get]
func (h *Handler) GetNurseryProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	nn, err := h.service.GetNurseryProfile(r.Context(), actor, nurseryID)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, nn)
}

// ---- Featured plants ----

// ListFeaturedPlants godoc
//
//	@Summary	List a nursery's featured plants
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	200	{object}	FeaturedPlantsResponse
//	@Router		/api/v1/nurseries/{nurseryId}/featured-plants [get]
func (h *Handler) ListFeaturedPlants(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	plants, err := h.service.ListFeaturedPlants(r.Context(), actor, nurseryID)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, FeaturedPlantsResponse{FeaturedPlants: plants})
}

// AddFeaturedPlant godoc
//
//	@Summary	Add a plant to nursery's featured list
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	201	{object}	FeaturedPlantResponse
//	@Router		/api/v1/nurseries/{nurseryId}/featured-plants [post]
func (h *Handler) AddFeaturedPlant(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	var req CreateFeaturedPlantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	fp, err := h.service.AddFeaturedPlant(r.Context(), actor, nurseryID, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, FeaturedPlantResponse{FeaturedPlant: fp})
}

// UpdateFeaturedPlant godoc
//
//	@Summary	Update a featured plant entry
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Param		featuredId	path	int	true	"Featured plant ID"
//	@Success	200	{object}	FeaturedPlantResponse
//	@Router		/api/v1/nurseries/{nurseryId}/featured-plants/{featuredId} [put]
func (h *Handler) UpdateFeaturedPlant(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	featuredID, ok := pathID(w, r, "featuredId")
	if !ok {
		return
	}
	var req UpdateFeaturedPlantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	fp, err := h.service.UpdateFeaturedPlant(r.Context(), actor, nurseryID, featuredID, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, FeaturedPlantResponse{FeaturedPlant: fp})
}

// DeleteFeaturedPlant godoc
//
//	@Summary	Remove a featured plant
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Param		featuredId	path	int	true	"Featured plant ID"
//	@Router		/api/v1/nurseries/{nurseryId}/featured-plants/{featuredId} [delete]
func (h *Handler) DeleteFeaturedPlant(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	featuredID, ok := pathID(w, r, "featuredId")
	if !ok {
		return
	}
	if err := h.service.DeleteFeaturedPlant(r.Context(), actor, nurseryID, featuredID); err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, map[string]string{"message": "featured plant removed"})
}

// ---- Sourcing posts ----

// ListPosts godoc
//
//	@Summary	List sourcing posts (NEED and AVAILABLE announcements)
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Success	200	{object}	PostsResponse
//	@Router		/api/v1/sourcing-posts [get]
func (h *Handler) ListPosts(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := listPostsQuery(r)
	posts, pg, err := h.service.ListPosts(r.Context(), actor, q)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, PostsResponse{Posts: posts, Pagination: pg})
}

// GetPost godoc
//
//	@Summary	Get a sourcing post
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Post ID"
//	@Success	200	{object}	PostWrapResponse
//	@Router		/api/v1/sourcing-posts/{id} [get]
func (h *Handler) GetPost(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	postID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := h.service.GetPost(r.Context(), actor, postID)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, PostWrapResponse{Post: p})
}

// CreatePost godoc
//
//	@Summary	Create a sourcing post (NEED or AVAILABLE)
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Success	201	{object}	PostWrapResponse
//	@Router		/api/v1/sourcing-posts [post]
func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreatePostRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := h.service.CreatePost(r.Context(), actor, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, PostWrapResponse{Post: p})
}

// UpdatePost godoc
//
//	@Summary	Update a sourcing post
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Post ID"
//	@Success	200	{object}	PostWrapResponse
//	@Router		/api/v1/sourcing-posts/{id} [put]
func (h *Handler) UpdatePost(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	postID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdatePostRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := h.service.UpdatePost(r.Context(), actor, postID, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, PostWrapResponse{Post: p})
}

// DeletePost godoc
//
//	@Summary	Delete a sourcing post
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Post ID"
//	@Router		/api/v1/sourcing-posts/{id} [delete]
func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	postID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeletePost(r.Context(), actor, postID); err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, map[string]string{"message": "post deleted"})
}

// ---- Post responses ----

// ListResponses godoc
//
//	@Summary	List responses to a sourcing post
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Post ID"
//	@Success	200	{object}	ResponsesWrap
//	@Router		/api/v1/sourcing-posts/{id}/responses [get]
func (h *Handler) ListResponses(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	postID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	responses, err := h.service.ListResponses(r.Context(), actor, postID)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, ResponsesWrap{Responses: responses})
}

// CreateResponse godoc
//
//	@Summary	Respond to a sourcing post
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Post ID"
//	@Success	201	{object}	ResponseWrap
//	@Router		/api/v1/sourcing-posts/{id}/responses [post]
func (h *Handler) CreateResponse(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	postID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req CreateResponseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := h.service.CreateResponse(r.Context(), actor, postID, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, ResponseWrap{Response: resp})
}

// UpdateResponse godoc
//
//	@Summary	Accept or decline a sourcing response
//	@Tags		Sourcing Posts
//	@Security	BearerAuth
//	@Param		id			path	int	true	"Post ID"
//	@Param		responseId	path	int	true	"Response ID"
//	@Success	200	{object}	ResponseWrap
//	@Router		/api/v1/sourcing-posts/{id}/responses/{responseId} [put]
func (h *Handler) UpdateResponse(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	postID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	responseID, ok := pathID(w, r, "responseId")
	if !ok {
		return
	}
	var req UpdateResponseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := h.service.UpdateResponse(r.Context(), actor, postID, responseID, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, ResponseWrap{Response: resp})
}

// ---- Internal helpers ----

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	c, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{
		UserID:    c.UserID,
		Roles:     c.Roles,
		IPAddress: c.IPAddress,
		UserAgent: c.UserAgent,
	}, true
}

func pathID(w http.ResponseWriter, r *http.Request, param string) (int64, bool) {
	v := chi.URLParam(r, param)
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_param", param+" must be a positive integer")
		return 0, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}

func nearbyQuery(r *http.Request) NearbyQuery {
	q := NearbyQuery{
		PlantName: r.URL.Query().Get("plant_name"),
		Page:      intParam(r, "page", 1),
		PerPage:   intParam(r, "per_page", 20),
		RadiusKM:  intParam(r, "radius_km", 50),
	}
	if lat := r.URL.Query().Get("latitude"); lat != "" {
		if v, err := strconv.ParseFloat(lat, 64); err == nil {
			q.Latitude = &v
		}
	}
	if lon := r.URL.Query().Get("longitude"); lon != "" {
		if v, err := strconv.ParseFloat(lon, 64); err == nil {
			q.Longitude = &v
		}
	}
	return q
}

func listPostsQuery(r *http.Request) ListPostsQuery {
	return ListPostsQuery{
		NurseryID: int64(intParam(r, "nursery_id", 0)),
		PostType:  r.URL.Query().Get("post_type"),
		Status:    r.URL.Query().Get("status"),
		PlantName: r.URL.Query().Get("plant_name"),
		Page:      intParam(r, "page", 1),
		PerPage:   intParam(r, "per_page", 20),
	}
}

func intParam(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func writeSourcingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		response.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}
