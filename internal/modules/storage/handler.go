package storage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/imagepolicy"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

const presignTTL = 15 * time.Minute

// Uploads clients may make directly: documents only. Photos go through POST
// /storage/images so each is checked against its image policy; the other
// buckets (plant and profile images, quotation and order PDFs) are written by
// the server only. The file extension comes from the type, never the
// client's file name.

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

// Attachments can be scanned documents.
const maxAttachmentBytes = 20 << 20

var clientUploads = map[string][]map[string]string{
	platformstorage.BucketAttachments: {documentTypes}, // private
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
	storage  *platformstorage.Client
	jwt      *jwtplatform.Service
	policies *imagepolicy.Store
	auditSvc *auditlog.Service
}

func NewHandler(s *platformstorage.Client, jwt *jwtplatform.Service, policies *imagepolicy.Store, auditSvc *auditlog.Service) *Handler {
	return &Handler{storage: s, jwt: jwt, policies: policies, auditSvc: auditSvc}
}

// Presign handles POST /api/v1/storage/presign
// Returns a pre-signed PUT URL so the client uploads directly to MinIO/S3.
func (h *Handler) Presign(w http.ResponseWriter, r *http.Request) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
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
	// Photos are checked by the API, so they are never presigned.
	if strings.HasPrefix(normalizeContentType(req.ContentType), "image/") {
		response.Error(w, 400, "use_image_upload", "photos are uploaded with POST /api/v1/storage/images (update the app)")
		return
	}
	if _, ok := clientUploads[req.Bucket]; !ok {
		response.Error(w, 400, "invalid_bucket", "bucket must be attachments (photos: POST /api/v1/storage/images)")
		return
	}
	// Checked normalized; signed as sent, since the client uploads with its own header.
	ext, ok := uploadExtension(req.Bucket, normalizeContentType(req.ContentType))
	if !ok {
		response.Error(w, 400, "invalid_content_type", "this file type cannot be uploaded here")
		return
	}

	if req.Size <= 0 || req.Size > maxAttachmentBytes {
		response.Error(w, 400, "file_too_large", fmt.Sprintf("files up to %d MB can be uploaded here", maxAttachmentBytes>>20))
		return
	}

	key := uuid.NewString() + ext
	if req.Bucket == platformstorage.BucketAttachments {
		// Private files carry their uploader, so a record can only point at
		// a file its own user uploaded (e.g. a driver's licence photo).
		key = platformstorage.UploaderPrefix(actor.UserID) + key
	}
	// The upload must send this exact Content-Type and size (both are signed into the URL).
	uploadURL, fileURL, err := h.storage.PresignPut(r.Context(), req.Bucket, key, req.ContentType, req.Size, presignTTL)
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
