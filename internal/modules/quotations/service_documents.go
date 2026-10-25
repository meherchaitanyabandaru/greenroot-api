// Uploaded quotation documents (PDF).

package quotations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

const maxPDFSize = 10 * 1024 * 1024 // 10 MB

// UploadDocument stores an official PDF for a quotation in MinIO and records metadata.
// If the quotation hasn't changed since the last upload, the existing document is reused.
// A new version is created when quotation.updated_at is newer than the current document.
func (s *Service) UploadDocument(ctx context.Context, actor ActorContext, quotationID int64, pdfBytes []byte) (QuotationDocument, string, error) {
	if int64(len(pdfBytes)) > maxPDFSize {
		return QuotationDocument{}, "", ErrFileTooLarge
	}

	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return QuotationDocument{}, "", err
	}
	// Admin/super-admin = read-only; drivers/buyers cannot upload
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") || actor.HasRole("DRIVER") {
		return QuotationDocument{}, "", ErrForbidden
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return QuotationDocument{}, "", err
	}

	// Idempotency: reuse existing PDF if quotation data hasn't changed since last upload.
	currentDoc, err := s.repository.GetCurrentDocument(ctx, quotationID)
	if err != nil {
		return QuotationDocument{}, "", err
	}
	if currentDoc != nil && !q.UpdatedAt.After(currentDoc.CreatedAt) {
		url, _ := s.storage.PresignGet(ctx, storage.BucketQuotationPDFs, currentDoc.ObjectKey, time.Hour)
		return *currentDoc, url, nil
	}

	hash := sha256.Sum256(pdfBytes)
	hashHex := hex.EncodeToString(hash[:])

	version := 1
	if currentDoc != nil {
		version = currentDoc.Version + 1
		if err := s.repository.MarkDocumentsNotCurrent(ctx, quotationID); err != nil {
			return QuotationDocument{}, "", fmt.Errorf("mark old documents: %w", err)
		}
	}

	nurseryID := int64(0)
	if q.NurseryID != nil {
		nurseryID = *q.NurseryID
	}
	objectKey := fmt.Sprintf("quotations/%d/%d/quotation-v%d.pdf", nurseryID, quotationID, version)

	if _, err := s.storage.PutObject(ctx, storage.BucketQuotationPDFs, objectKey, "application/pdf", pdfBytes); err != nil {
		return QuotationDocument{}, "", fmt.Errorf("upload PDF to storage: %w", err)
	}

	generatedByName, _ := s.repository.GetUserName(ctx, actor.UserID)
	doc := QuotationDocument{
		QuotationID:     quotationID,
		Version:         version,
		ObjectKey:       objectKey,
		SHA256Hash:      hashHex,
		MimeType:        "application/pdf",
		FileSize:        int64(len(pdfBytes)),
		GeneratedBy:     &actor.UserID,
		GeneratedByName: &generatedByName,
		IsCurrent:       true,
	}
	created, err := s.repository.CreateDocument(ctx, doc)
	if err != nil {
		return QuotationDocument{}, "", fmt.Errorf("store document metadata: %w", err)
	}

	url, _ := s.storage.PresignGet(ctx, storage.BucketQuotationPDFs, objectKey, time.Hour)

	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, auditlog.ActionUpload, map[string]any{
		"doc_id":  created.DocID,
		"version": version,
		"sha256":  hashHex,
		"size":    created.FileSize,
	})

	return *created, url, nil
}

// GetCurrentDocument returns the current official PDF for a quotation + a 1-hour presigned GET URL.
func (s *Service) GetCurrentDocument(ctx context.Context, actor ActorContext, quotationID int64) (QuotationDocument, string, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return QuotationDocument{}, "", err
	}
	if err := s.canView(ctx, actor, *q); err != nil {
		return QuotationDocument{}, "", err
	}

	doc, err := s.repository.GetCurrentDocument(ctx, quotationID)
	if err != nil {
		return QuotationDocument{}, "", err
	}
	if doc == nil {
		return QuotationDocument{}, "", ErrDocumentNotFound
	}

	url, err := s.storage.PresignGet(ctx, storage.BucketQuotationPDFs, doc.ObjectKey, time.Hour)
	if err != nil {
		return *doc, "", fmt.Errorf("presign URL: %w", err)
	}
	return *doc, url, nil
}

// ListDocuments returns all PDF versions for a quotation (owner and admin only).
func (s *Service) ListDocuments(ctx context.Context, actor ActorContext, quotationID int64) ([]QuotationDocument, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return nil, err
	}
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		if !s.isNurseryOwner(ctx, actor, *q) {
			return nil, ErrForbidden
		}
	}
	return s.repository.ListDocuments(ctx, quotationID)
}
