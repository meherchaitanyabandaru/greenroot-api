package notifications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	List(ctx context.Context, input ListNotificationsRequest) ([]Notification, int64, error)
	FindByID(ctx context.Context, id int64) (*Notification, error)
	Create(ctx context.Context, input CreateNotificationInput) (*Notification, error)
	MarkRead(ctx context.Context, id int64) (*Notification, error)
	MarkAllRead(ctx context.Context, userID int64) error
	Delete(ctx context.Context, id int64) error
	ListDevices(ctx context.Context, userID int64) ([]Device, error)
	UpsertDevice(ctx context.Context, userID int64, input DeviceRequest) (*Device, error)
	DeleteDevice(ctx context.Context, id int64, userID int64, admin bool) error
	ListTemplates(ctx context.Context) ([]Template, error)
	CreateTemplate(ctx context.Context, input TemplateRequest) (*Template, error)
	UpdateTemplate(ctx context.Context, id int64, input TemplateRequest) (*Template, error)
	DeleteTemplate(ctx context.Context, id int64) error
}

type CreateNotificationInput struct {
	UserID     *int64
	Type       string
	TemplateID *int64
	Title      *string
	Message    *string
	Channel    string
	Status     string
	DataJSON   string
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }

func (r *PostgresRepository) List(ctx context.Context, input ListNotificationsRequest) ([]Notification, int64, error) {
	where, args := buildNotificationWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.notifications "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(notificationSelect()+` %s ORDER BY notification_id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Notification, 0)
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, id int64) (*Notification, error) {
	item, err := scanNotification(r.db.QueryRowContext(ctx, notificationSelect()+" WHERE notification_id = $1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &item, err
}

func (r *PostgresRepository) Create(ctx context.Context, input CreateNotificationInput) (*Notification, error) {
	notificationCode, err := publiccode.Next(ctx, r.db, publiccode.Notifications, time.Now())
	if err != nil {
		return nil, err
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO public.notifications (notification_code, user_id, template_id, notification_type, title, message, channel, notification_status, data, sent_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, NULLIF($9, '')::jsonb,
			CASE WHEN $10 = 'SENT' THEN CURRENT_TIMESTAMP ELSE NULL END, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING notification_id
	`, notificationCode, int64OrNil(input.UserID), int64OrNil(input.TemplateID), input.Type, stringOrEmpty(input.Title), stringOrEmpty(input.Message), input.Channel, input.Status, input.DataJSON, input.Status).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) MarkRead(ctx context.Context, id int64) (*Notification, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE public.notifications SET notification_status = 'READ', read_at = COALESCE(read_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP WHERE notification_id = $1`, id)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE public.notifications SET notification_status = 'READ', read_at = COALESCE(read_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}

func (r *PostgresRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM public.notifications WHERE notification_id = $1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListDevices(ctx context.Context, userID int64) ([]Device, error) {
	rows, err := r.db.QueryContext(ctx, deviceSelect()+" WHERE user_id = $1 AND is_active = true ORDER BY device_id DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (r *PostgresRepository) UpsertDevice(ctx context.Context, userID int64, input DeviceRequest) (*Device, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO public.user_notification_devices (user_id, fcm_token, device_type, device_id_external, platform, app_version, is_active, last_seen_at, created_at, updated_at)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (fcm_token) DO UPDATE SET user_id = EXCLUDED.user_id, device_type = EXCLUDED.device_type,
			device_id_external = EXCLUDED.device_id_external, platform = EXCLUDED.platform, app_version = EXCLUDED.app_version,
			is_active = true, last_seen_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		RETURNING device_id
	`, userID, input.FCMToken, stringOrEmpty(input.DeviceType), stringOrEmpty(input.ExternalDeviceID), stringOrEmpty(input.Platform), stringOrEmpty(input.AppVersion)).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.findDevice(ctx, id)
}

func (r *PostgresRepository) DeleteDevice(ctx context.Context, id int64, userID int64, admin bool) error {
	query := `UPDATE public.user_notification_devices SET is_active = false, updated_at = CURRENT_TIMESTAMP WHERE device_id = $1`
	args := []any{id}
	if !admin {
		query += " AND user_id = $2"
		args = append(args, userID)
	}
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListTemplates(ctx context.Context) ([]Template, error) {
	rows, err := r.db.QueryContext(ctx, templateSelect()+" ORDER BY template_id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Template, 0)
	for rows.Next() {
		item, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) CreateTemplate(ctx context.Context, input TemplateRequest) (*Template, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `INSERT INTO public.notification_templates (template_code, template_name, channel, subject, message_template, is_active, created_at, updated_at) VALUES ($1, NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) RETURNING template_id`, input.Code, stringOrEmpty(input.Name), input.Channel, stringOrEmpty(input.Subject), stringOrEmpty(input.MessageTemplate), input.IsActive).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.findTemplate(ctx, id)
}

func (r *PostgresRepository) UpdateTemplate(ctx context.Context, id int64, input TemplateRequest) (*Template, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE public.notification_templates SET template_code=$2, template_name=NULLIF($3,''), channel=$4, subject=NULLIF($5,''), message_template=NULLIF($6,''), is_active=$7, updated_at=CURRENT_TIMESTAMP WHERE template_id=$1`, id, input.Code, stringOrEmpty(input.Name), input.Channel, stringOrEmpty(input.Subject), stringOrEmpty(input.MessageTemplate), input.IsActive)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrNotFound
	}
	return r.findTemplate(ctx, id)
}

func (r *PostgresRepository) DeleteTemplate(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE public.notification_templates SET is_active=false, updated_at=CURRENT_TIMESTAMP WHERE template_id=$1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}


func (r *PostgresRepository) findDevice(ctx context.Context, id int64) (*Device, error) {
	d, err := scanDevice(r.db.QueryRowContext(ctx, deviceSelect()+" WHERE device_id=$1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}
func (r *PostgresRepository) findTemplate(ctx context.Context, id int64) (*Template, error) {
	t, err := scanTemplate(r.db.QueryRowContext(ctx, templateSelect()+" WHERE template_id=$1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

func notificationSelect() string {
	return `SELECT notification_id,notification_code,user_id,template_id,notification_type,title,message,COALESCE(channel::text,''),COALESCE(notification_status::text,''),data::text,sent_at,read_at,created_at,updated_at FROM public.notifications`
}
func deviceSelect() string {
	return `SELECT device_id,user_id,fcm_token,device_type,device_id_external,platform,app_version,is_active,last_seen_at,created_at,updated_at FROM public.user_notification_devices`
}
func templateSelect() string {
	return `SELECT template_id,template_code,template_name,COALESCE(channel::text,''),subject,message_template,COALESCE(is_active,true),created_at,updated_at FROM public.notification_templates`
}

func buildNotificationWhere(input ListNotificationsRequest) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0)
	if input.UserID > 0 {
		args = append(args, input.UserID)
		clauses = append(clauses, fmt.Sprintf("user_id=$%d", len(args)))
	}
	if input.Type != "" {
		args = append(args, input.Type)
		clauses = append(clauses, fmt.Sprintf("notification_type=$%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("notification_status::text=$%d", len(args)))
	}
	if input.Channel != "" {
		args = append(args, input.Channel)
		clauses = append(clauses, fmt.Sprintf("channel::text=$%d", len(args)))
	}
	if input.Unread != nil {
		if *input.Unread {
			clauses = append(clauses, "read_at IS NULL")
		} else {
			clauses = append(clauses, "read_at IS NOT NULL")
		}
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(notification_code ILIKE $%d OR title ILIKE $%d OR message ILIKE $%d)", len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func scanNotification(row interface{ Scan(...any) error }) (Notification, error) {
	var n Notification
	var userID, templateID sql.NullInt64
	var title, msg, data sql.NullString
	var sent, read, updated sql.NullTime
	err := row.Scan(&n.ID, &n.NotificationCode, &userID, &templateID, &n.Type, &title, &msg, &n.Channel, &n.Status, &data, &sent, &read, &n.CreatedAt, &updated)
	if err != nil {
		return n, err
	}
	n.UserID = nullableInt64(userID)
	n.TemplateID = nullableInt64(templateID)
	n.Title = nullableString(title)
	n.Message = nullableString(msg)
	n.Data = nullableString(data)
	if sent.Valid {
		n.SentAt = &sent.Time
	}
	if read.Valid {
		n.ReadAt = &read.Time
	}
	if updated.Valid {
		n.UpdatedAt = &updated.Time
	}
	return n, nil
}
func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	var dt, ext, platform, version sql.NullString
	var seen, updated sql.NullTime
	err := row.Scan(&d.ID, &d.UserID, &d.FCMToken, &dt, &ext, &platform, &version, &d.IsActive, &seen, &d.CreatedAt, &updated)
	if err != nil {
		return d, err
	}
	d.DeviceType = nullableString(dt)
	d.ExternalDeviceID = nullableString(ext)
	d.Platform = nullableString(platform)
	d.AppVersion = nullableString(version)
	if seen.Valid {
		d.LastSeenAt = &seen.Time
	}
	if updated.Valid {
		d.UpdatedAt = &updated.Time
	}
	return d, nil
}
func scanTemplate(row interface{ Scan(...any) error }) (Template, error) {
	var t Template
	var name, subject, msg sql.NullString
	var updated sql.NullTime
	err := row.Scan(&t.ID, &t.Code, &name, &t.Channel, &subject, &msg, &t.IsActive, &t.CreatedAt, &updated)
	if err != nil {
		return t, err
	}
	t.Name = nullableString(name)
	t.Subject = nullableString(subject)
	t.MessageTemplate = nullableString(msg)
	if updated.Valid {
		t.UpdatedAt = &updated.Time
	}
	return t, nil
}
func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}
func nullableInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
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
