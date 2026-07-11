package storage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

const presignTTL = 15 * time.Minute

type Handler struct {
	storage *platformstorage.Client
	jwt     *jwtplatform.Service
}

func NewHandler(s *platformstorage.Client, jwt *jwtplatform.Service) *Handler {
	return &Handler{storage: s, jwt: jwt}
}

// Presign handles POST /api/v1/storage/presign
// Returns a pre-signed PUT URL so the client uploads directly to MinIO/S3.
func (h *Handler) Presign(w http.ResponseWriter, r *http.Request) {
	_, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return
	}

	var req PresignRequest
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, 400, "invalid_json", "invalid request body")
		return
	}

	req.Bucket = strings.TrimSpace(req.Bucket)
	req.FileName = strings.TrimSpace(req.FileName)
	req.ContentType = strings.TrimSpace(req.ContentType)

	if req.Bucket == "" || req.FileName == "" {
		response.Error(w, 400, "invalid_input", "bucket and file_name are required")
		return
	}
	if !platformstorage.IsValidBucket(req.Bucket) {
		response.Error(w, 400, "invalid_bucket", "bucket must be one of: profile-images, plant-images, loading-photos, delivery-photos, attachments, market-ads, nursery-logos")
		return
	}

	ext := filepath.Ext(req.FileName)
	key := fmt.Sprintf("%s%s", uuid.NewString(), ext)

	uploadURL, fileURL, err := h.storage.PresignPut(r.Context(), req.Bucket, key, presignTTL)
	if err != nil {
		response.Error(w, 500, "storage_error", "could not generate upload URL")
		return
	}

	response.JSON(w, http.StatusOK, PresignResponse{
		UploadURL:    uploadURL,
		FileURL:      fileURL,
		Key:          key,
		Bucket:       req.Bucket,
		ExpiresInSec: int(presignTTL.Seconds()),
	})
}
