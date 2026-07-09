// Package auditlog provides a centralised, fire-and-forget audit service used
// by every business module.  Two surfaces are exposed:
//
//   - Service.Log           — business events (orders, payments, plants …)
//   - Service.LogSecurity   — auth / security events (login, permission denied …)
//
// Design rules:
//   - Writes use context.Background() so a client-cancelled request never
//     silently drops an audit row.
//   - Failures are logged internally and never surfaced to callers; audit
//     must never fail a customer request.
//   - Only changed fields should be passed in OldValue / NewValue — not whole
//     objects — to keep JSONB payloads compact.
package auditlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Entry describes a single business audit event.
type Entry struct {
	UserID      int64  // actor performing the action
	NurseryID   int64  // tenant (0 = derive from context or leave NULL)
	Module      Module // e.g. ModuleOrders
	EntityType  string // e.g. "order", "order_item", "plant"
	EntityID    int64  // primary key of the affected record
	Action      Action // e.g. ActionCreate, ActionUpdate
	Description string // human-readable summary, e.g. "Order #42 status → DISPATCHED"
	OldValue    any    // only changed fields (JSONB); nil = not applicable
	NewValue    any    // only changed fields (JSONB)
	Metadata    any    // extra context (JSONB); nil = omit
	IPAddress   string
	DeviceInfo  string // user-agent or structured device string
}

// SecurityEntry describes a security-sensitive event.
type SecurityEntry struct {
	UserID      int64
	NurseryID   int64
	EventType   SecurityEvent
	Description string
	Metadata    any
	IPAddress   string
	DeviceInfo  string
}

// Service writes audit events for both business and security tables.
type Service struct {
	db  *sql.DB
	log *slog.Logger
}

func NewService(db *sql.DB, log *slog.Logger) *Service {
	return &Service{db: db, log: log}
}

// Log inserts a business audit event.  Never returns an error — failures are
// logged at ERROR level; the calling module must not check the return value.
// Safe to call on a nil receiver (no-op); useful in unit tests.
func (s *Service) Log(ctx context.Context, e Entry) {
	if s == nil {
		return
	}
	requestID := RequestIDFromContext(ctx)
	nurseryID := e.NurseryID
	if nurseryID == 0 {
		nurseryID = NurseryIDFromContext(ctx)
	}

	const q = `
		INSERT INTO public.audit_logs (
			request_id, user_id, nursery_id,
			module, entity_type, record_id,
			action_type, description,
			old_data, new_data, metadata,
			source_ip, user_agent,
			changed_by, table_name, changed_at
		) VALUES (
			NULLIF($1,''),  NULLIF($2,0),  NULLIF($3,0),
			NULLIF($4,''),  NULLIF($5,''), $6,
			$7,             NULLIF($8,''),
			$9::jsonb,      $10::jsonb,    $11::jsonb,
			NULLIF($12,''), NULLIF($13,''),
			NULLIF($14,0),  NULLIF($15,''), $16
		)`

	_, err := s.db.ExecContext(
		context.Background(), q,
		requestID, e.UserID, nurseryID,
		e.Module, e.EntityType, e.EntityID,
		e.Action, e.Description,
		toJSONB(e.OldValue), toJSONB(e.NewValue), toJSONB(e.Metadata),
		e.IPAddress, e.DeviceInfo,
		e.UserID, e.EntityType, time.Now(),
	)
	if err != nil {
		s.log.Error("audit log write failed",
			"error", err,
			"module", e.Module,
			"action", e.Action,
			"entity_id", e.EntityID,
		)
	}
}

// LogSecurity inserts a security audit event.
// Safe to call on a nil receiver (no-op); useful in unit tests.
func (s *Service) LogSecurity(ctx context.Context, e SecurityEntry) {
	if s == nil {
		return
	}
	requestID := RequestIDFromContext(ctx)
	nurseryID := e.NurseryID
	if nurseryID == 0 {
		nurseryID = NurseryIDFromContext(ctx)
	}

	const q = `
		INSERT INTO public.security_audit_logs (
			request_id, user_id, nursery_id,
			event_type, description, metadata,
			ip_address, device_info, created_at
		) VALUES (
			NULLIF($1,''), NULLIF($2,0), NULLIF($3,0),
			$4,            NULLIF($5,''), $6::jsonb,
			NULLIF($7,''), $8::jsonb,    $9
		)`

	_, err := s.db.ExecContext(
		context.Background(), q,
		requestID, e.UserID, nurseryID,
		e.EventType, e.Description, toJSONB(e.Metadata),
		e.IPAddress, toJSONB(e.DeviceInfo), time.Now(),
	)
	if err != nil {
		s.log.Error("security audit log write failed",
			"error", err,
			"event", e.EventType,
			"user_id", e.UserID,
		)
	}
}

// toJSONB marshals v to a *string suitable for $N::jsonb parameters.
// Passing nil produces a SQL NULL; invalid values produce a JSON error object.
func toJSONB(v any) *string {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		s := fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
		return &s
	}
	str := string(b)
	return &str
}
