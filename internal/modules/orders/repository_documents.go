// Verification tokens and generated documents of an order.
package orders

import (
	"context"
	"database/sql"
	"errors"
)

func (r *PostgresRepository) GetActiveVerificationToken(ctx context.Context, orderID int64) (*OrderVerification, error) {
	const q = `
		SELECT verification_id, order_id, token, status, created_at, revoked_at, revoked_by_user_id
		FROM public.order_verifications
		WHERE order_id = $1 AND status = 'ACTIVE'
	`
	return scanOrderVerification(r.db.QueryRowContext(ctx, q, orderID))
}

func (r *PostgresRepository) GetVerificationByToken(ctx context.Context, token string) (*OrderVerification, error) {
	const q = `
		SELECT verification_id, order_id, token, status, created_at, revoked_at, revoked_by_user_id
		FROM public.order_verifications
		WHERE token = $1
	`
	return scanOrderVerification(r.db.QueryRowContext(ctx, q, token))
}

func (r *PostgresRepository) CreateVerificationToken(ctx context.Context, orderID int64, token string) (*OrderVerification, error) {
	const q = `
		INSERT INTO public.order_verifications (order_id, token, status)
		VALUES ($1, $2, 'ACTIVE')
		RETURNING verification_id, order_id, token, status, created_at, revoked_at, revoked_by_user_id
	`
	return scanOrderVerification(r.db.QueryRowContext(ctx, q, orderID, token))
}

func (r *PostgresRepository) RevokeVerificationTokens(ctx context.Context, orderID int64, revokedByUserID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE public.order_verifications
		SET status = 'REVOKED', revoked_at = CURRENT_TIMESTAMP, revoked_by_user_id = $2
		WHERE order_id = $1 AND status = 'ACTIVE'
	`, orderID, revokedByUserID)
	return err
}

func (r *PostgresRepository) CreateDocument(ctx context.Context, doc OrderDocument) (*OrderDocument, error) {
	const q = `
		INSERT INTO public.order_documents
			(order_id, version, object_key, sha256_hash, mime_type, file_size, generated_by_user_id, generated_by_name, is_current)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, TRUE)
		RETURNING doc_id, order_id, version, object_key, sha256_hash, mime_type, file_size,
		          generated_by_user_id, generated_by_name, is_current, created_at
	`
	var d OrderDocument
	var generatedBy sql.NullInt64
	var generatedByName sql.NullString
	if doc.GeneratedBy != nil {
		generatedBy = sql.NullInt64{Int64: *doc.GeneratedBy, Valid: true}
	}
	if doc.GeneratedByName != nil {
		generatedByName = sql.NullString{String: *doc.GeneratedByName, Valid: true}
	}
	err := r.db.QueryRowContext(ctx, q,
		doc.OrderID, doc.Version, doc.ObjectKey, doc.SHA256Hash,
		doc.MimeType, doc.FileSize, generatedBy, generatedByName,
	).Scan(
		&d.DocID, &d.OrderID, &d.Version, &d.ObjectKey, &d.SHA256Hash,
		&d.MimeType, &d.FileSize, &generatedBy, &generatedByName, &d.IsCurrent, &d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if generatedBy.Valid {
		d.GeneratedBy = &generatedBy.Int64
	}
	if generatedByName.Valid {
		d.GeneratedByName = &generatedByName.String
	}
	return &d, nil
}

func (r *PostgresRepository) GetCurrentDocument(ctx context.Context, orderID int64) (*OrderDocument, error) {
	const q = `
		SELECT doc_id, order_id, version, object_key, sha256_hash, mime_type, file_size,
		       generated_by_user_id, generated_by_name, is_current, created_at
		FROM public.order_documents
		WHERE order_id = $1 AND is_current = TRUE
	`
	var d OrderDocument
	var generatedBy sql.NullInt64
	var generatedByName sql.NullString
	err := r.db.QueryRowContext(ctx, q, orderID).Scan(
		&d.DocID, &d.OrderID, &d.Version, &d.ObjectKey, &d.SHA256Hash,
		&d.MimeType, &d.FileSize, &generatedBy, &generatedByName, &d.IsCurrent, &d.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if generatedBy.Valid {
		d.GeneratedBy = &generatedBy.Int64
	}
	if generatedByName.Valid {
		d.GeneratedByName = &generatedByName.String
	}
	return &d, nil
}

func (r *PostgresRepository) MarkDocumentsNotCurrent(ctx context.Context, orderID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.order_documents SET is_current = FALSE WHERE order_id = $1 AND is_current = TRUE`,
		orderID,
	)
	return err
}
