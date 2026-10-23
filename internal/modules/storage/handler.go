package storage

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

const presignTTL = 15 * time.Minute

// Uploads clients may make directly, and what each bucket accepts. Other
// buckets (plant and profile images, quotation and order PDFs) are written
// by the server only. The file extension comes from the type, never the
// client's file name.
var imageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/heic": ".heic",
	"image/heif": ".heif",
	"image/gif":  ".gif",
}

var documentTypes = map[string]string{
	"application/pdf":    ".pdf",
	"application/msword": ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
	"application/vnd.ms-excel": ".xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ".xlsx",
	"text/csv":                 ".csv",
	"text/plain":               ".txt",
	"application/octet-stream": ".bin", // private bucket: always downloaded, never rendered
}

var clientUploads = map[string][]map[string]string{
	platformstorage.BucketAttachments:    {imageTypes, documentTypes}, // private
	platformstorage.BucketDeliveryPhotos: {imageTypes},                // private
	platformstorage.BucketMarketAds:      {imageTypes},                // public
	platformstorage.BucketNurseryLogos:   {imageTypes},                // public
}

// uploadExtension says whether bucket takes contentType and with which extension.
func uploadExtension(bucket, contentType string) (string, bool) {
	for _, types := range clientUploads[bucket] {
		if ext, ok := types[contentType]; ok {
			return ext, true
		}
	}
	return "", false
}

// normalizeContentType lower-cases, drops parameters and fixes "image/jpg".
func normalizeContentType(ct string) string {
	ct, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(ct)), ";")
	ct = strings.TrimSpace(ct)
	if ct == "image/jpg" || ct == "image/pjpeg" {
		return "image/jpeg"
	}
	return ct
}

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
	if _, ok := clientUploads[req.Bucket]; !ok {
		response.Error(w, 400, "invalid_bucket", "bucket must be one of: attachments, delivery-photos, market-ads, nursery-logos")
		return
	}
	// Checked normalized; signed as sent, since the client uploads with its own header.
	ext, ok := uploadExtension(req.Bucket, normalizeContentType(req.ContentType))
	if !ok {
		response.Error(w, 400, "invalid_content_type", "this file type cannot be uploaded here")
		return
	}

	key := uuid.NewString() + ext
	// The upload must send this exact Content-Type (it is signed into the URL).
	uploadURL, fileURL, err := h.storage.PresignPut(r.Context(), req.Bucket, key, req.ContentType, presignTTL)
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
