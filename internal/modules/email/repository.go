package email

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var (
	ErrNotFound     = apperrs.ErrNotFound
	ErrConflict     = apperrs.ErrConflict
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
)

type Repository interface {
	GetSender(ctx context.Context) (*Sender, error)
	UpsertSender(ctx context.Context, in SenderRequest) (*Sender, error)
	ListTemplates(ctx context.Context) ([]Template, error)
	FindTemplate(ctx context.Context, id int64) (*Template, error)
	FindTemplateByCode(ctx context.Context, code string) (*Template, error)
	CreateTemplate(ctx context.Context, in TemplateRequest) (*Template, error)
	UpdateTemplate(ctx context.Context, id int64, in TemplateRequest) (*Template, error)
	InsertMessage(ctx context.Context, m Message) (int64, error)
	ListMessages(ctx context.Context, in ListMessagesRequest) ([]Message, int64, error)
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }

// ── Sender ──────────────────────────────────────────────────────────────────

const senderColumns = `sender_id, from_name, from_address, reply_to, domain_verified, updated_at`

func scanSender(row interface{ Scan(...any) error }) (*Sender, error) {
	var s Sender
	err := row.Scan(&s.ID, &s.FromName, &s.FromAddress, &s.ReplyTo, &s.DomainVerified, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *PostgresRepository) GetSender(ctx context.Context) (*Sender, error) {
	return scanSender(r.db.QueryRowContext(ctx, "SELECT "+senderColumns+" FROM public.email_sender LIMIT 1"))
}

// UpsertSender relies on the singleton unique index ((true)).
func (r *PostgresRepository) UpsertSender(ctx context.Context, in SenderRequest) (*Sender, error) {
	return scanSender(r.db.QueryRowContext(ctx, `
		INSERT INTO public.email_sender (from_name, from_address, reply_to, domain_verified)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT ((true)) DO UPDATE SET
			from_name = EXCLUDED.from_name, from_address = EXCLUDED.from_address,
			reply_to = EXCLUDED.reply_to, domain_verified = EXCLUDED.domain_verified,
			updated_at = CURRENT_TIMESTAMP
		RETURNING `+senderColumns, in.FromName, in.FromAddress, in.ReplyTo, in.DomainVerified))
}

// ── Templates ───────────────────────────────────────────────────────────────

const templateColumns = `email_template_id, template_code, template_name, subject, body_html, body_text, is_active, created_at, updated_at`

func scanTemplate(row interface{ Scan(...any) error }) (*Template, error) {
	var t Template
	err := row.Scan(&t.ID, &t.TemplateCode, &t.TemplateName, &t.Subject, &t.BodyHTML, &t.BodyText, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.IsSystem = isSystemTemplate(t.TemplateCode)
	if t.Variables, err = t.spec().Variables(); err != nil {
		t.Variables = []string{}
	}
	return &t, nil
}

func (r *PostgresRepository) ListTemplates(ctx context.Context) ([]Template, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+templateColumns+" FROM public.email_templates ORDER BY email_template_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Template, 0)
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *t)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) FindTemplate(ctx context.Context, id int64) (*Template, error) {
	return scanTemplate(r.db.QueryRowContext(ctx, "SELECT "+templateColumns+" FROM public.email_templates WHERE email_template_id = $1", id))
}

func (r *PostgresRepository) FindTemplateByCode(ctx context.Context, code string) (*Template, error) {
	return scanTemplate(r.db.QueryRowContext(ctx, "SELECT "+templateColumns+" FROM public.email_templates WHERE template_code = $1", code))
}

func activeOr(v *bool) bool { return v == nil || *v }

func (r *PostgresRepository) CreateTemplate(ctx context.Context, in TemplateRequest) (*Template, error) {
	t, err := scanTemplate(r.db.QueryRowContext(ctx, `
		INSERT INTO public.email_templates (template_code, template_name, subject, body_html, body_text, is_active)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+templateColumns,
		in.TemplateCode, in.TemplateName, in.Subject, in.BodyHTML, in.BodyText, activeOr(in.IsActive)))
	return t, mapUnique(err)
}

func (r *PostgresRepository) UpdateTemplate(ctx context.Context, id int64, in TemplateRequest) (*Template, error) {
	t, err := scanTemplate(r.db.QueryRowContext(ctx, `
		UPDATE public.email_templates SET template_code = $2, template_name = $3, subject = $4, body_html = $5,
			body_text = $6, is_active = $7, updated_at = CURRENT_TIMESTAMP
		WHERE email_template_id = $1 RETURNING `+templateColumns,
		id, in.TemplateCode, in.TemplateName, in.Subject, in.BodyHTML, in.BodyText, activeOr(in.IsActive)))
	return t, mapUnique(err)
}

// ── Message log ─────────────────────────────────────────────────────────────

func (r *PostgresRepository) InsertMessage(ctx context.Context, m Message) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO public.email_messages (email_template_id, template_code, user_id, to_email, subject, body,
			provider, provider_message_id, status, error_code, error_message, reference_type, reference_id, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING email_message_id`,
		m.TemplateID, m.TemplateCode, m.UserID, m.ToEmail, m.Subject, m.Body, m.Provider, m.ProviderMessageID,
		m.Status, m.ErrorCode, m.ErrorMessage, m.ReferenceType, m.ReferenceID, m.SentAt).Scan(&id)
	return id, err
}

func (r *PostgresRepository) ListMessages(ctx context.Context, in ListMessagesRequest) ([]Message, int64, error) {
	clauses, args := []string{}, []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if in.Status != "" {
		add("status = $%d", in.Status)
	}
	if in.TemplateCode != "" {
		add("template_code = $%d", in.TemplateCode)
	}
	if in.ToEmail != "" {
		add("to_email LIKE '%%' || $%d || '%%'", in.ToEmail)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.email_messages"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, in.PerPage, (in.Page-1)*in.PerPage)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT email_message_id, email_template_id, template_code, user_id, to_email, subject, body, provider,
			provider_message_id, status, error_code, error_message, reference_type, reference_id, sent_at, created_at
		FROM public.email_messages%s ORDER BY email_message_id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.TemplateID, &m.TemplateCode, &m.UserID, &m.ToEmail, &m.Subject, &m.Body, &m.Provider,
			&m.ProviderMessageID, &m.Status, &m.ErrorCode, &m.ErrorMessage, &m.ReferenceType, &m.ReferenceID, &m.SentAt, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	return items, total, rows.Err()
}

func mapUnique(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
