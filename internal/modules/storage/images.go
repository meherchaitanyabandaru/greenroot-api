package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/imagepolicy"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

// Photos are uploaded through the API (never a presigned URL), so every one
// is checked against its category's policy before it is stored. Selfies go
// through POST /users/me/avatar, which applies the SELFIE policy.

// imageBuckets: where each category is stored; private buckets keep the
// uploader in the key so a record can only point at its own uploads.
var imageBuckets = map[string]string{
	imagepolicy.MarketAd:        platformstorage.BucketMarketAds,      // public
	imagepolicy.NurseryBranding: platformstorage.BucketNurseryLogos,   // public
	imagepolicy.DeliveryProof:   platformstorage.BucketDeliveryPhotos, // private
	imagepolicy.LoadingProof:    platformstorage.BucketDeliveryPhotos, // private
	imagepolicy.DriverLicence:   platformstorage.BucketAttachments,    // private
	imagepolicy.Attachment:      platformstorage.BucketAttachments,    // private
}

var publicBuckets = map[string]bool{platformstorage.BucketMarketAds: true, platformstorage.BucketNurseryLogos: true}

type ImageUploadResponse struct {
	FileURL       string `json:"file_url"`
	Key           string `json:"key"`
	Bucket        string `json:"bucket"`
	Category      string `json:"category"`
	ContentType   string `json:"content_type"`
	Size          int    `json:"size"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	PolicyVersion int    `json:"policy_version"`
}

// UploadImage handles POST /storage/images (multipart: category, file).
func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return
	}
	if h.storage == nil {
		response.Error(w, http.StatusServiceUnavailable, "storage_unavailable", "photo uploads are not available right now")
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_form", "send multipart form fields category and file")
		return
	}
	var category string
	var data []byte
	for data == nil {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_form", "could not read the upload")
			return
		}
		switch part.FormName() {
		case "category":
			raw, _ := io.ReadAll(io.LimitReader(part, 64))
			category = strings.ToUpper(strings.TrimSpace(string(raw)))
		case "file":
			// category must come first: its policy decides how much to read.
			bucket, known := imageBuckets[category]
			if !known || bucket == "" {
				response.Error(w, http.StatusBadRequest, "invalid_category", "category must come before file and be one of: "+strings.Join(imageCategories(), ", "))
				return
			}
			policy := h.policies.Get(r.Context(), category)
			data, err = io.ReadAll(io.LimitReader(part, policy.MaxBytes+1))
			if err != nil {
				response.Error(w, http.StatusBadRequest, "invalid_form", "could not read the upload")
				return
			}
		}
		_ = part.Close()
	}
	if data == nil {
		response.Error(w, http.StatusBadRequest, "missing_file", "file is required")
		return
	}
	policy := h.policies.Get(r.Context(), category)
	info, err := imagepolicy.Check(policy, data)
	if err != nil {
		response.Error(w, http.StatusUnprocessableEntity, imageErrorCode(err), imagepolicy.Message(policy, err))
		return
	}
	bucket := imageBuckets[category]
	key := uuid.NewString() + info.Ext
	if !publicBuckets[bucket] {
		key = platformstorage.UploaderPrefix(actor.UserID) + key
	}
	fileURL, err := h.storage.PutObject(r.Context(), bucket, key, info.ContentType, data)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "storage_error", "could not store the photo")
		return
	}
	response.JSON(w, http.StatusCreated, ImageUploadResponse{
		FileURL: fileURL, Key: key, Bucket: bucket, Category: category, ContentType: info.ContentType,
		Size: len(data), Width: info.Width, Height: info.Height, PolicyVersion: policy.Version,
	})
}

func imageCategories() []string {
	out := make([]string, 0, len(imageBuckets))
	for _, c := range imagepolicy.Categories {
		if _, ok := imageBuckets[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// imageErrorCode is the API code for a refused image.
func imageErrorCode(err error) string {
	switch {
	case errors.Is(err, imagepolicy.ErrTooLarge):
		return "file_too_large"
	case errors.Is(err, imagepolicy.ErrFormat):
		return "invalid_image_format"
	case errors.Is(err, imagepolicy.ErrTooSmall):
		return "image_too_small"
	case errors.Is(err, imagepolicy.ErrTooBig), errors.Is(err, imagepolicy.ErrTooManyPixels):
		return "image_too_big"
	default:
		return "invalid_image"
	}
}

// ListPolicies handles GET /image-policies: what the apps compress to.
func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := authctx.FromRequest(w, r, h.jwt); !ok {
		return
	}
	response.OK(w, map[string]any{"policies": h.policies.All(r.Context())})
}

type adminPolicy struct {
	imagepolicy.Policy
	Default imagepolicy.Policy `json:"default"`
	Bounds  policyBounds       `json:"bounds"`
}

type policyBounds struct {
	MaxSideMin    int   `json:"max_side_min"`
	MaxSideMax    int   `json:"max_side_max"`
	MinSideMin    int   `json:"min_side_min"`
	QualityMin    int   `json:"quality_min"`
	QualityMax    int   `json:"quality_max"`
	MaxBytesMin   int64 `json:"max_bytes_min"`
	MaxBytesMax   int64 `json:"max_bytes_max"`
	HardMaxPixels int   `json:"hard_max_pixels"`
}

func boundsJSON(c string) policyBounds {
	b := imagepolicy.BoundsFor(c)
	return policyBounds{b.MaxSideMin, b.MaxSideMax, b.MinSideMin, b.QualityMin, b.QualityMax, b.MaxBytesMin, b.MaxBytesMax, imagepolicy.HardMaxPixels}
}

func isAdmin(a authctx.ActorContext) bool { return a.HasRole("ADMIN") || a.HasRole("SUPER_ADMIN") }

// AdminListPolicies handles GET /admin/image-policies: current values, the
// safe defaults and the bounds each field must stay within.
func (h *Handler) AdminListPolicies(w http.ResponseWriter, r *http.Request) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return
	}
	if !isAdmin(actor.AsActorContext()) {
		response.Error(w, http.StatusForbidden, "forbidden", "admins only")
		return
	}
	out := make([]adminPolicy, 0, len(imagepolicy.Categories))
	for _, p := range h.policies.All(r.Context()) {
		out = append(out, adminPolicy{Policy: p, Default: imagepolicy.Defaults[p.Category], Bounds: boundsJSON(p.Category)})
	}
	response.OK(w, map[string]any{"policies": out, "can_edit": actor.AsActorContext().HasRole("SUPER_ADMIN")})
}

type updatePolicyRequest struct {
	MaxSide              int      `json:"max_side"`
	MinSide              int      `json:"min_side"`
	Quality              int      `json:"quality"`
	MaxBytes             int64    `json:"max_bytes"`
	AllowedFormats       []string `json:"allowed_formats"`
	IsCompressionEnabled bool     `json:"is_compression_enabled"`
	// Version is the version the admin edited; a newer saved one is 409.
	Version    int     `json:"version"`
	ChangeNote *string `json:"change_note"`
}

// AdminUpdatePolicy handles PUT /admin/image-policies/{category} (super admin).
func (h *Handler) AdminUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return
	}
	ac := actor.AsActorContext()
	if !ac.HasRole("SUPER_ADMIN") {
		response.Error(w, http.StatusForbidden, "forbidden", "only a super admin can change image policies")
		return
	}
	category := strings.ToUpper(chi.URLParam(r, "category"))
	if !imagepolicy.IsCategory(category) {
		response.Error(w, http.StatusNotFound, "not_found", "unknown image category")
		return
	}
	var req updatePolicyRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	if req.ChangeNote == nil || len(strings.TrimSpace(*req.ChangeNote)) < 5 {
		response.Error(w, http.StatusBadRequest, "change_note_required", "say why the policy changes (at least 5 characters)")
		return
	}
	formats := make([]string, 0, len(req.AllowedFormats))
	for _, f := range req.AllowedFormats {
		formats = append(formats, strings.ToUpper(strings.TrimSpace(f)))
	}
	slices.Sort(formats)
	old := h.policies.Get(r.Context(), category)
	p := imagepolicy.Policy{Category: category, MaxSide: req.MaxSide, MinSide: req.MinSide, Quality: req.Quality,
		MaxBytes: req.MaxBytes, AllowedFormats: formats, IsCompressionEnabled: req.IsCompressionEnabled}
	saved, err := h.policies.Update(r.Context(), actor.UserID, p, req.Version, req.ChangeNote)
	switch {
	case errors.Is(err, imagepolicy.ErrUnsafePolicy):
		response.Error(w, http.StatusBadRequest, "unsafe_policy", err.Error())
		return
	case errors.Is(err, imagepolicy.ErrVersionConflict):
		response.Error(w, http.StatusConflict, "version_conflict", "someone else changed this policy; reload and try again")
		return
	case err != nil:
		response.Error(w, http.StatusInternalServerError, "image_policy_error", "could not save the policy")
		return
	}
	h.audit(r, ac, category, old, saved, req.ChangeNote)
	response.OK(w, map[string]any{"policy": adminPolicy{Policy: saved, Default: imagepolicy.Defaults[category], Bounds: boundsJSON(category)}})
}

// AdminPolicyHistory handles GET /admin/image-policies/{category}/history.
func (h *Handler) AdminPolicyHistory(w http.ResponseWriter, r *http.Request) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return
	}
	if !isAdmin(actor.AsActorContext()) {
		response.Error(w, http.StatusForbidden, "forbidden", "admins only")
		return
	}
	category := strings.ToUpper(chi.URLParam(r, "category"))
	if !imagepolicy.IsCategory(category) {
		response.Error(w, http.StatusNotFound, "not_found", "unknown image category")
		return
	}
	history, err := h.policies.History(r.Context(), category, 50)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "image_policy_error", "could not read the history")
		return
	}
	response.OK(w, map[string]any{"history": history})
}

func (h *Handler) audit(r *http.Request, actor authctx.ActorContext, category string, old, saved imagepolicy.Policy, note *string) {
	if h.auditSvc == nil {
		return
	}
	snapshot := func(p imagepolicy.Policy) map[string]any {
		var m map[string]any
		raw, _ := json.Marshal(p)
		_ = json.NewDecoder(bytes.NewReader(raw)).Decode(&m)
		return m
	}
	h.auditSvc.Log(r.Context(), auditlog.Entry{
		UserID: actor.UserID, Module: auditlog.ModuleStorage, EntityType: "image_policy_" + strings.ToLower(category),
		EntityID: int64(saved.Version), // the record is that category's policy version
		Action:   auditlog.ActionUpdate, Description: "Image policy " + category + " v" + strconv.Itoa(saved.Version) + ": " + strings.TrimSpace(*note),
		OldValue: snapshot(old), NewValue: snapshot(saved), IPAddress: actor.IPAddress, DeviceInfo: actor.UserAgent,
	})
}
