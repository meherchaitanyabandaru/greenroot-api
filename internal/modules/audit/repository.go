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
}
type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }
func (r *PostgresRepository) List(ctx context.Context, in ListRequest) ([]AuditLog, int64, error) {
	where, args := where(in)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.audit_logs "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	off := (in.Page - 1) * in.PerPage
	args = append(args, in.PerPage, off)
	q := fmt.Sprintf(`SELECT audit_id,table_name,record_id,action_type,old_data::text,new_data::text,changed_by,source_ip,user_agent,changed_at FROM public.audit_logs %s ORDER BY audit_id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AuditLog{}
	for rows.Next() {
		var a AuditLog
		var oldd, newd, ip, ua sql.NullString
		var by sql.NullInt64
		if err := rows.Scan(&a.ID, &a.TableName, &a.RecordID, &a.Action, &oldd, &newd, &by, &ip, &ua, &a.ChangedAt); err != nil {
			return nil, 0, err
		}
		if oldd.Valid {
			a.OldData = &oldd.String
		}
		if newd.Valid {
			a.NewData = &newd.String
		}
		if by.Valid {
			a.ChangedBy = &by.Int64
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
func where(in ListRequest) (string, []any) {
	c := []string{"1=1"}
	a := []any{}
	if in.TableName != "" {
		a = append(a, in.TableName)
		c = append(c, fmt.Sprintf("table_name=$%d", len(a)))
	}
	if in.Action != "" {
		a = append(a, in.Action)
		c = append(c, fmt.Sprintf("action_type=$%d", len(a)))
	}
	if in.ChangedBy > 0 {
		a = append(a, in.ChangedBy)
		c = append(c, fmt.Sprintf("changed_by=$%d", len(a)))
	}
	if in.RecordID > 0 {
		a = append(a, in.RecordID)
		c = append(c, fmt.Sprintf("record_id=$%d", len(a)))
	}
	return "WHERE " + strings.Join(c, " AND "), a
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
	in.Action = strings.ToUpper(strings.TrimSpace(in.Action))
	in.TableName = strings.TrimSpace(in.TableName)
	return in
}
func pages(total int64, per int) int {
	if per <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(per)))
}
