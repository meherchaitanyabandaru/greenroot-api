package sms

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	GetEntity(ctx context.Context) (*Entity, error)
	UpsertEntity(ctx context.Context, input EntityRequest) (*Entity, error)
	ListHeaders(ctx context.Context) ([]Header, error)
	FindHeader(ctx context.Context, id int64) (*Header, error)
	CreateHeader(ctx context.Context, input HeaderRequest) (*Header, error)
	UpdateHeader(ctx context.Context, id int64, input HeaderRequest) (*Header, error)
	ListTemplates(ctx context.Context) ([]Template, error)
	FindTemplate(ctx context.Context, id int64) (*Template, error)
	FindTemplateByCode(ctx context.Context, code string) (*Template, error)
	CreateTemplate(ctx context.Context, input TemplateRequest) (*Template, error)
	UpdateTemplate(ctx context.Context, id int64, input TemplateRequest) (*Template, error)
	InsertMessage(ctx context.Context, msg Message) (int64, error)
	ListMessages(ctx context.Context, input ListMessagesRequest) ([]Message, int64, error)
	GetPreferences(ctx context.Context, userID int64) (Preferences, error)
	UpsertPreferences(ctx context.Context, userID int64, optIn bool, source string, at time.Time) (Preferences, error)
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }

// ── Principal Entity ────────────────────────────────────────────────────────

const entitySelect = `SELECT entity_id, pe_id, entity_name, dlt_operator, telemarketer_name, telemarketer_id,
	registration_status, notes, created_at, updated_at FROM public.sms_dlt_entities`

func scanEntity(row interface{ Scan(...any) error }) (*Entity, error) {
	var e Entity
	err := row.Scan(&e.ID, &e.PEID, &e.EntityName, &e.DLTOperator, &e.TelemarketerName, &e.TelemarketerID,
		&e.RegistrationStatus, &e.Notes, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

func (r *PostgresRepository) GetEntity(ctx context.Context) (*Entity, error) {
	return scanEntity(r.db.QueryRowContext(ctx, entitySelect+" LIMIT 1"))
}

// UpsertEntity relies on the singleton unique index ((true)).
func (r *PostgresRepository) UpsertEntity(ctx context.Context, in EntityRequest) (*Entity, error) {
	return scanEntity(r.db.QueryRowContext(ctx, `
		INSERT INTO public.sms_dlt_entities (pe_id, entity_name, dlt_operator, telemarketer_name, telemarketer_id, registration_status, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT ((true)) DO UPDATE SET
			pe_id = EXCLUDED.pe_id, entity_name = EXCLUDED.entity_name, dlt_operator = EXCLUDED.dlt_operator,
			telemarketer_name = EXCLUDED.telemarketer_name, telemarketer_id = EXCLUDED.telemarketer_id,
			registration_status = EXCLUDED.registration_status, notes = EXCLUDED.notes, updated_at = CURRENT_TIMESTAMP
		RETURNING entity_id, pe_id, entity_name, dlt_operator, telemarketer_name, telemarketer_id,
			registration_status, notes, created_at, updated_at`,
		in.PEID, in.EntityName, in.DLTOperator, in.TelemarketerName, in.TelemarketerID, in.RegistrationStatus, in.Notes))
}

// ── Headers ─────────────────────────────────────────────────────────────────

const headerColumns = `header_id, header, header_type, dlt_header_id, registration_status, rejection_reason, created_at, updated_at`

func scanHeader(row interface{ Scan(...any) error }) (*Header, error) {
	var h Header
	err := row.Scan(&h.ID, &h.Header, &h.HeaderType, &h.DLTHeaderID, &h.RegistrationStatus, &h.RejectionReason, &h.CreatedAt, &h.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &h, err
}

func (r *PostgresRepository) ListHeaders(ctx context.Context) ([]Header, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+headerColumns+" FROM public.sms_headers ORDER BY header_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Header, 0)
	for rows.Next() {
		h, err := scanHeader(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *h)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) FindHeader(ctx context.Context, id int64) (*Header, error) {
	return scanHeader(r.db.QueryRowContext(ctx, "SELECT "+headerColumns+" FROM public.sms_headers WHERE header_id = $1", id))
}

func (r *PostgresRepository) CreateHeader(ctx context.Context, in HeaderRequest) (*Header, error) {
	h, err := scanHeader(r.db.QueryRowContext(ctx, `
		INSERT INTO public.sms_headers (header, header_type, dlt_header_id, registration_status, rejection_reason)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+headerColumns,
		in.Header, in.HeaderType, in.DLTHeaderID, in.RegistrationStatus, in.RejectionReason))
	return h, mapUnique(err)
}

func (r *PostgresRepository) UpdateHeader(ctx context.Context, id int64, in HeaderRequest) (*Header, error) {
	h, err := scanHeader(r.db.QueryRowContext(ctx, `
		UPDATE public.sms_headers SET header = $2, header_type = $3, dlt_header_id = $4, registration_status = $5,
			rejection_reason = $6, updated_at = CURRENT_TIMESTAMP
		WHERE header_id = $1 RETURNING `+headerColumns,
		id, in.Header, in.HeaderType, in.DLTHeaderID, in.RegistrationStatus, in.RejectionReason))
	return h, mapUnique(err)
}

// ── Templates ───────────────────────────────────────────────────────────────

const templateSelect = `SELECT t.sms_template_id, t.template_code, t.template_name, t.category, t.header_id,
	h.header, h.header_type, h.registration_status, t.body, t.variables, t.is_unicode, t.dlt_template_id,
	t.registration_status, t.rejection_reason, t.whitelisted_urls, t.whitelisted_numbers, t.is_active,
	t.approved_at, t.created_at, t.updated_at
	FROM public.sms_templates t LEFT JOIN public.sms_headers h ON h.header_id = t.header_id`

func scanTemplate(row interface{ Scan(...any) error }) (*Template, error) {
	var t Template
	var vars, urls, numbers []byte
	err := row.Scan(&t.ID, &t.TemplateCode, &t.TemplateName, &t.Category, &t.HeaderID,
		&t.Header, &t.HeaderType, &t.HeaderStatus, &t.Body, &vars, &t.IsUnicode, &t.DLTTemplateID,
		&t.RegistrationStatus, &t.RejectionReason, &urls, &numbers, &t.IsActive,
		&t.ApprovedAt, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, f := range []struct {
		raw  []byte
		dest *[]string
	}{{vars, &t.Variables}, {urls, &t.WhitelistedURLs}, {numbers, &t.WhitelistedNumbers}} {
		*f.dest = []string{}
		if len(f.raw) > 0 {
			if err := json.Unmarshal(f.raw, f.dest); err != nil {
				return nil, err
			}
		}
	}
	t.IsSystem = isSystemTemplate(t.TemplateCode)
	return &t, nil
}

func (r *PostgresRepository) ListTemplates(ctx context.Context) ([]Template, error) {
	rows, err := r.db.QueryContext(ctx, templateSelect+" ORDER BY t.sms_template_id")
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
	return scanTemplate(r.db.QueryRowContext(ctx, templateSelect+" WHERE t.sms_template_id = $1", id))
}

func (r *PostgresRepository) FindTemplateByCode(ctx context.Context, code string) (*Template, error) {
	return scanTemplate(r.db.QueryRowContext(ctx, templateSelect+" WHERE t.template_code = $1", code))
}

func (r *PostgresRepository) CreateTemplate(ctx context.Context, in TemplateRequest) (*Template, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO public.sms_templates (template_code, template_name, category, header_id, body, variables, is_unicode,
			dlt_template_id, registration_status, rejection_reason, whitelisted_urls, whitelisted_numbers, is_active, approved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::varchar, $10, $11, $12, $13, CASE WHEN $9::varchar = 'APPROVED' THEN CURRENT_TIMESTAMP END)
		RETURNING sms_template_id`,
		templateArgs(in)...).Scan(&id)
	if err != nil {
		return nil, mapUnique(err)
	}
	return r.FindTemplate(ctx, id)
}

func (r *PostgresRepository) UpdateTemplate(ctx context.Context, id int64, in TemplateRequest) (*Template, error) {
	args := append([]any{id}, templateArgs(in)...)
	res, err := r.db.ExecContext(ctx, `
		UPDATE public.sms_templates SET template_code = $2, template_name = $3, category = $4, header_id = $5, body = $6,
			variables = $7, is_unicode = $8, dlt_template_id = $9, registration_status = $10::varchar, rejection_reason = $11,
			whitelisted_urls = $12, whitelisted_numbers = $13, is_active = $14,
			approved_at = CASE WHEN $10::varchar = 'APPROVED' THEN COALESCE(approved_at, CURRENT_TIMESTAMP) END,
			updated_at = CURRENT_TIMESTAMP
		WHERE sms_template_id = $1`, args...)
	if err != nil {
		return nil, mapUnique(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return r.FindTemplate(ctx, id)
}

func templateArgs(in TemplateRequest) []any {
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	return []any{in.TemplateCode, in.TemplateName, in.Category, in.HeaderID, in.Body, mustJSONArray(in.Variables),
		in.IsUnicode, in.DLTTemplateID, in.RegistrationStatus, in.RejectionReason,
		mustJSONArray(in.WhitelistedURLs), mustJSONArray(in.WhitelistedNumbers), active}
}

func mustJSONArray(items []string) string {
	if items == nil {
		items = []string{}
	}
	data, _ := json.Marshal(items)
	return string(data)
}

// ── Message log ─────────────────────────────────────────────────────────────

func (r *PostgresRepository) InsertMessage(ctx context.Context, m Message) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO public.sms_messages (sms_template_id, template_code, user_id, mobile, message_body, header,
			dlt_entity_id, dlt_template_id, category, parts, provider, provider_message_id, status,
			error_code, error_message, reference_type, reference_id, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING sms_message_id`,
		m.TemplateID, m.TemplateCode, m.UserID, m.Mobile, m.MessageBody, m.Header, m.DLTEntityID, m.DLTTemplateID,
		m.Category, m.Parts, m.Provider, m.ProviderMessageID, m.Status, m.ErrorCode, m.ErrorMessage,
		m.ReferenceType, m.ReferenceID, m.SentAt).Scan(&id)
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
	if in.Mobile != "" {
		add("mobile LIKE '%%' || $%d || '%%'", in.Mobile)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.sms_messages"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, in.PerPage, (in.Page-1)*in.PerPage)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT sms_message_id, sms_template_id, template_code, user_id, mobile, message_body, header, dlt_entity_id,
			dlt_template_id, category, parts, provider, provider_message_id, status, error_code, error_message,
			reference_type, reference_id, sent_at, created_at
		FROM public.sms_messages%s ORDER BY sms_message_id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.TemplateID, &m.TemplateCode, &m.UserID, &m.Mobile, &m.MessageBody, &m.Header,
			&m.DLTEntityID, &m.DLTTemplateID, &m.Category, &m.Parts, &m.Provider, &m.ProviderMessageID, &m.Status,
			&m.ErrorCode, &m.ErrorMessage, &m.ReferenceType, &m.ReferenceID, &m.SentAt, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	return items, total, rows.Err()
}

// ── Preferences ─────────────────────────────────────────────────────────────

const preferenceColumns = `user_id, promotional_opt_in, promotional_consent_at, promotional_revoked_at, consent_source, updated_at`

func scanPreferences(row interface{ Scan(...any) error }) (Preferences, error) {
	var p Preferences
	err := row.Scan(&p.UserID, &p.PromotionalOptIn, &p.PromotionalConsentAt, &p.PromotionalRevokedAt, &p.ConsentSource, &p.UpdatedAt)
	return p, err
}

// GetPreferences returns the opt-out default when the user never chose.
func (r *PostgresRepository) GetPreferences(ctx context.Context, userID int64) (Preferences, error) {
	p, err := scanPreferences(r.db.QueryRowContext(ctx, "SELECT "+preferenceColumns+" FROM public.sms_preferences WHERE user_id = $1", userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Preferences{UserID: userID}, nil
	}
	return p, err
}

// UpsertPreferences stamps consent/revocation times only when the choice changes.
func (r *PostgresRepository) UpsertPreferences(ctx context.Context, userID int64, optIn bool, source string, at time.Time) (Preferences, error) {
	return scanPreferences(r.db.QueryRowContext(ctx, `
		INSERT INTO public.sms_preferences (user_id, promotional_opt_in, promotional_consent_at, promotional_revoked_at, consent_source, updated_at)
		VALUES ($1, $2, CASE WHEN $2 THEN $4::timestamptz END, CASE WHEN NOT $2 THEN $4::timestamptz END, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET
			promotional_consent_at = CASE WHEN EXCLUDED.promotional_opt_in AND NOT sms_preferences.promotional_opt_in THEN EXCLUDED.updated_at ELSE sms_preferences.promotional_consent_at END,
			promotional_revoked_at = CASE WHEN NOT EXCLUDED.promotional_opt_in AND sms_preferences.promotional_opt_in THEN EXCLUDED.updated_at ELSE sms_preferences.promotional_revoked_at END,
			promotional_opt_in = EXCLUDED.promotional_opt_in,
			consent_source = EXCLUDED.consent_source,
			updated_at = EXCLUDED.updated_at
		RETURNING `+preferenceColumns, userID, optIn, source, at))
}

func mapUnique(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
