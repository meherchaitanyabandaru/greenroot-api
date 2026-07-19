package audit

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
)

type Repository interface {
	List(context.Context, ListRequest) ([]AuditLog, int64, error)
	ListSecurity(context.Context, ListSecurityRequest) ([]SecurityLog, int64, error)
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }

func (r *PostgresRepository) List(ctx context.Context, in ListRequest) ([]AuditLog, int64, error) {
	where, args := buildWhere(in)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.audit_logs "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	off := (in.Page - 1) * in.PerPage
	args = append(args, in.PerPage, off)
	q := fmt.Sprintf(`
		SELECT audit_id, COALESCE(module,''), COALESCE(entity_type,''),
		       record_id, action_type,
		       description, old_data::text, new_data::text,
		       user_id, request_id, nursery_id,
		       source_ip, user_agent, changed_at
		FROM public.audit_logs %s
		ORDER BY audit_id DESC
		LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []AuditLog{}
	for rows.Next() {
		var a AuditLog
		var desc, oldd, newd, ip, ua, reqID sql.NullString
		var userID, nurseryID sql.NullInt64
		if err := rows.Scan(
			&a.ID, &a.Module, &a.EntityType,
			&a.RecordID, &a.Action,
			&desc, &oldd, &newd,
			&userID, &reqID, &nurseryID,
			&ip, &ua, &a.ChangedAt,
		); err != nil {
			return nil, 0, err
		}
		if desc.Valid {
			a.Description = &desc.String
		}
		if oldd.Valid {
			a.OldData = &oldd.String
		}
		if newd.Valid {
			a.NewData = &newd.String
		}
		if userID.Valid {
			a.UserID = &userID.Int64
		}
		if reqID.Valid {
			a.RequestID = &reqID.String
		}
		if nurseryID.Valid {
			a.NurseryID = &nurseryID.Int64
		}
		if ip.Valid {
			a.SourceIP = &ip.String
		}
		if ua.Valid {
			a.UserAgent = &ua.String
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (r *PostgresRepository) ListSecurity(ctx context.Context, in ListSecurityRequest) ([]SecurityLog, int64, error) {
	conds := []string{"1=1"}
	args := []any{}
	if in.EventType != "" {
		args = append(args, strings.ToUpper(in.EventType))
		conds = append(conds, fmt.Sprintf("event_type=$%d", len(args)))
	}
	if in.UserID > 0 {
		args = append(args, in.UserID)
		conds = append(conds, fmt.Sprintf("user_id=$%d", len(args)))
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.security_audit_logs "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	off := (in.Page - 1) * in.PerPage
	args = append(args, in.PerPage, off)
	q := fmt.Sprintf(`
		SELECT id, event_type, description,
		       user_id, nursery_id, request_id,
		       ip_address, device_info::text, metadata::text,
		       created_at
		FROM public.security_audit_logs %s
		ORDER BY id DESC
		LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []SecurityLog{}
	for rows.Next() {
		var s SecurityLog
		var desc, deviceInfo, metadata, reqID, ip sql.NullString
		var userID, nurseryID sql.NullInt64
		if err := rows.Scan(
			&s.ID, &s.EventType, &desc,
			&userID, &nurseryID, &reqID,
			&ip, &deviceInfo, &metadata,
			&s.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if desc.Valid {
			s.Description = &desc.String
		}
		if userID.Valid {
			s.UserID = &userID.Int64
		}
		if nurseryID.Valid {
			s.NurseryID = &nurseryID.Int64
		}
		if reqID.Valid {
			s.RequestID = &reqID.String
		}
		if ip.Valid {
			s.IPAddress = &ip.String
		}
		if deviceInfo.Valid {
			s.DeviceInfo = &deviceInfo.String
		}
		if metadata.Valid {
			s.Metadata = &metadata.String
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

func buildWhere(in ListRequest) (string, []any) {
	conds := []string{"1=1"}
	args := []any{}
	if in.Module != "" {
		args = append(args, strings.ToUpper(in.Module))
		conds = append(conds, fmt.Sprintf("module=$%d", len(args)))
	}
	if in.EntityType != "" {
		args = append(args, strings.ToLower(in.EntityType))
		conds = append(conds, fmt.Sprintf("entity_type=$%d", len(args)))
	}
	if in.Action != "" {
		args = append(args, strings.ToUpper(in.Action))
		conds = append(conds, fmt.Sprintf("action_type=$%d", len(args)))
	}
	if in.UserID > 0 {
		args = append(args, in.UserID)
		conds = append(conds, fmt.Sprintf("user_id=$%d", len(args)))
	}
	if in.RecordID > 0 {
		args = append(args, in.RecordID)
		conds = append(conds, fmt.Sprintf("record_id=$%d", len(args)))
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func normalize(in ListRequest) ListRequest {
	if in.Page <= 0 {
		in.Page = 1
	}
	if in.PerPage <= 0 {
		in.PerPage = 20
	}
	if in.PerPage > 100 {
		in.PerPage = 100
	}
	return in
}

func pages(total int64, per int) int {
	if per <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(per)))
}
