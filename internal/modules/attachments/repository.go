package attachments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = apperrs.ErrNotFound

type Repository interface {
	List(ctx context.Context, input ListRequest) ([]Attachment, int64, error)
	FindByID(ctx context.Context, id int64) (*Attachment, error)
	Create(ctx context.Context, actorID int64, input AttachmentRequest) (*Attachment, error)
	Delete(ctx context.Context, id int64) error
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }

func (r *PostgresRepository) List(ctx context.Context, input ListRequest) ([]Attachment, int64, error) {
	where, args := buildWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.attachments "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(`SELECT attachment_id, attachment_code, entity_type, entity_id, file_name, file_url, file_type, file_size, uploaded_by, uploaded_at FROM public.attachments %s ORDER BY attachment_id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Attachment, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, id int64) (*Attachment, error) {
	item, err := scan(r.db.QueryRowContext(ctx, `SELECT attachment_id, attachment_code, entity_type, entity_id, file_name, file_url, file_type, file_size, uploaded_by, uploaded_at FROM public.attachments WHERE attachment_id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &item, err
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input AttachmentRequest) (*Attachment, error) {
	attachmentCode, err := publiccode.Next(ctx, r.db, publiccode.Attachments, time.Now())
	if err != nil {
		return nil, err
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `INSERT INTO public.attachments (attachment_code,entity_type,entity_id,file_name,file_url,file_type,file_size,uploaded_by,uploaded_at) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,CURRENT_TIMESTAMP) RETURNING attachment_id`, attachmentCode, input.EntityType, input.EntityID, input.FileName, input.FileURL, stringOrEmpty(input.FileType), int64OrNil(input.FileSize), actorID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM public.attachments WHERE attachment_id=$1`, id)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func buildWhere(input ListRequest) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0)
	if input.EntityType != "" {
		args = append(args, input.EntityType)
		clauses = append(clauses, fmt.Sprintf("entity_type=$%d", len(args)))
	}
	if input.EntityID > 0 {
		args = append(args, input.EntityID)
		clauses = append(clauses, fmt.Sprintf("entity_id=$%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(attachment_code ILIKE $%d OR file_name ILIKE $%d OR file_url ILIKE $%d)", len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func scan(row interface{ Scan(...any) error }) (Attachment, error) {
	var a Attachment
	var ft sql.NullString
	var fs, by sql.NullInt64
	err := row.Scan(&a.ID, &a.AttachmentCode, &a.EntityType, &a.EntityID, &a.FileName, &a.FileURL, &ft, &fs, &by, &a.UploadedAt)
	if err != nil {
		return a, err
	}
	if ft.Valid {
		a.FileType = &ft.String
	}
	if fs.Valid {
		a.FileSize = &fs.Int64
	}
	if by.Valid {
		a.UploadedBy = &by.Int64
	}
	return a, nil
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func int64OrNil(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
