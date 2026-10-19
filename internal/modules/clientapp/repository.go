package clientapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

// Repository stores and lists crash reports.
type Repository interface {
	InsertCrashReport(ctx context.Context, report CrashReport) (int64, error)
	ListCrashReports(ctx context.Context, in ListCrashReportsRequest) ([]CrashReport, int64, error)
}

// PostgresRepository is the Repository backed by public.client_crash_reports.
type PostgresRepository struct{ db *sql.DB }

// NewRepository returns a PostgreSQL repository.
func NewRepository(db *sql.DB) *PostgresRepository { return &PostgresRepository{db: db} }

const crashColumns = `client_crash_report_id, user_id, platform, app_version, build_number,
	os_version, device_model, error_type, message, stack_trace, context, is_fatal,
	occurred_at, received_at, request_id`

// InsertCrashReport stores a scrubbed report and returns its id.
func (r *PostgresRepository) InsertCrashReport(ctx context.Context, c CrashReport) (int64, error) {
	ctxJSON, err := json.Marshal(c.Context)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO public.client_crash_reports (
			user_id, platform, app_version, build_number, os_version, device_model,
			error_type, message, stack_trace, context, is_fatal, occurred_at, request_id)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''),
			$7, $8, NULLIF($9, ''), $10::jsonb, $11, $12, NULLIF($13, ''))
		RETURNING client_crash_report_id`,
		c.UserID, c.Platform, c.AppVersion, c.BuildNumber, c.OSVersion, c.DeviceModel,
		c.ErrorType, c.Message, c.StackTrace, string(ctxJSON), c.IsFatal, c.OccurredAt, c.RequestID,
	).Scan(&id)
	return id, pgerr.Map(err)
}

// ListCrashReports returns the newest reports first.
func (r *PostgresRepository) ListCrashReports(ctx context.Context, in ListCrashReportsRequest) ([]CrashReport, int64, error) {
	var (
		clauses []string
		args    []any
	)
	if in.AppVersion != "" {
		args = append(args, in.AppVersion)
		clauses = append(clauses, fmt.Sprintf("app_version = $%d", len(args)))
	}
	if in.Platform != "" {
		args = append(args, in.Platform)
		clauses = append(clauses, fmt.Sprintf("platform = $%d", len(args)))
	}
	if in.FatalOnly {
		clauses = append(clauses, "is_fatal")
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int64
	// #nosec G202 -- where is built from fixed fragments with $n placeholders; values are bound args.
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM public.client_crash_reports"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, in.PerPage, (in.Page-1)*in.PerPage)
	// #nosec G201 -- where is built from fixed fragments with $n placeholders; values are bound args.
	q := fmt.Sprintf("SELECT %s FROM public.client_crash_reports%s ORDER BY received_at DESC, client_crash_report_id DESC LIMIT $%d OFFSET $%d",
		crashColumns, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []CrashReport
	for rows.Next() {
		var (
			c                                          CrashReport
			userID                                     sql.NullInt64
			build, osVersion, device, stack, requestID sql.NullString
			ctxJSON                                    []byte
		)
		if err := rows.Scan(&c.ID, &userID, &c.Platform, &c.AppVersion, &build, &osVersion, &device,
			&c.ErrorType, &c.Message, &stack, &ctxJSON, &c.IsFatal, &c.OccurredAt, &c.ReceivedAt, &requestID); err != nil {
			return nil, 0, err
		}
		if userID.Valid {
			c.UserID = &userID.Int64
		}
		c.BuildNumber, c.OSVersion, c.DeviceModel = build.String, osVersion.String, device.String
		c.StackTrace, c.RequestID = stack.String, requestID.String
		if err := json.Unmarshal(ctxJSON, &c.Context); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}
