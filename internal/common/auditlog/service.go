// Package auditlog provides a centralised, fire-and-forget audit service used
// by every business module.  Two surfaces are exposed:
//
//   - Service.Log           — business events (orders, payments, plants …)
//   - Service.LogSecurity   — auth / security events (login, permission denied …)
//
// Design rules:
//   - Writes are queued on a buffered channel and executed by a single background
//     goroutine, so callers never block on a DB round-trip.
//   - If the queue is full (DB down, spike) the event is dropped and logged at WARN;
//     audit must never slow a customer request.
//   - Failures are logged internally and never surfaced to callers.
//   - Call Close() after the HTTP server shuts down to drain pending writes.
package auditlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type writeJob struct {
	query string
	args  []any
}

// Service writes audit events for both business and security tables.
type Service struct {
	db    *sql.DB
	log   *slog.Logger
	queue chan writeJob
	wg    sync.WaitGroup
}

func NewService(db *sql.DB, log *slog.Logger) *Service {
	s := &Service{
		db:    db,
		log:   log,
		queue: make(chan writeJob, 512),
	}
	s.wg.Add(1)
	go s.drain()
	return s
}

// Close drains all pending audit writes then returns.
// Call this after the HTTP server has finished serving in-flight requests.
func (s *Service) Close() {
	if s == nil {
		return
	}
	close(s.queue)
	s.wg.Wait()
}

func (s *Service) drain() {
	defer s.wg.Done()
	for job := range s.queue {
		if _, err := s.db.ExecContext(context.Background(), job.query, job.args...); err != nil {
			s.log.Error("audit write failed", "error", err)
		}
	}
}

func (s *Service) enqueue(q string, args []any) {
	select {
	case s.queue <- writeJob{query: q, args: args}:
	default:
		s.log.Warn("audit queue full — event dropped")
	}
}

// Entry describes a single business audit event.
type Entry struct {
	UserID      int64  // actor performing the action
	NurseryID   int64  // tenant (0 = derive from context or leave NULL)
	Module      Module // e.g. ModuleOrders
	EntityType  string // e.g. EntityOrder, EntityPlant
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

// Log queues a business audit event. Never blocks; never returns an error.
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
			changed_by, changed_at
		) VALUES (
			NULLIF($1,''),  NULLIF($2,0),  NULLIF($3,0),
			NULLIF($4,''),  NULLIF($5,''), $6,
			$7,             NULLIF($8,''),
			$9::jsonb,      $10::jsonb,    $11::jsonb,
			NULLIF($12,''), NULLIF($13,''),
			NULLIF($14,0),  $15
		)`

	s.enqueue(q, []any{
		requestID, e.UserID, nurseryID,
		e.Module, e.EntityType, e.EntityID,
		e.Action, e.Description,
		toJSONB(e.OldValue), toJSONB(e.NewValue), toJSONB(e.Metadata),
		e.IPAddress, e.DeviceInfo,
		e.UserID, time.Now(),
	})
}

// LogSecurity queues a security audit event. Never blocks; never returns an error.
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

	s.enqueue(q, []any{
		requestID, e.UserID, nurseryID,
		e.EventType, e.Description, toJSONB(e.Metadata),
		e.IPAddress, toJSONB(e.DeviceInfo), time.Now(),
	})
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
