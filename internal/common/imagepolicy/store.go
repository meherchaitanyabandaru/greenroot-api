package imagepolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// cacheTTL: how long an API instance keeps policies before re-reading them.
// An admin change reaches every instance within this time.
const cacheTTL = time.Minute

var ErrVersionConflict = errors.New("policy was changed by someone else")

// Store reads policies from public.image_policies, cached per instance, and
// falls back to the last good read (or Defaults) when the database fails, so
// uploads keep working with safe rules.
type Store struct {
	db     *sql.DB
	mu     sync.Mutex
	cached map[string]Policy
	at     time.Time
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Get returns category's policy (Defaults for an unknown or missing row).
func (s *Store) Get(ctx context.Context, category string) Policy {
	all := s.load(ctx)
	if p, ok := all[category]; ok {
		return p
	}
	return Defaults[category]
}

// All returns every category's policy in display order.
func (s *Store) All(ctx context.Context) []Policy {
	all := s.load(ctx)
	out := make([]Policy, 0, len(Categories))
	for _, c := range Categories {
		if p, ok := all[c]; ok {
			out = append(out, p)
		} else {
			out = append(out, Defaults[c])
		}
	}
	return out
}

func (s *Store) load(ctx context.Context) map[string]Policy {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.at) < cacheTTL {
		return s.cached
	}
	fresh, err := s.read(ctx)
	if err != nil {
		slog.Warn("image policies unavailable; using the last known or default policies", "error", err)
		if s.cached != nil {
			return s.cached
		}
		return Defaults
	}
	s.cached, s.at = fresh, time.Now()
	return fresh
}

func (s *Store) read(ctx context.Context) (map[string]Policy, error) {
	if s.db == nil {
		return Defaults, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT category, max_side, min_side, quality, max_bytes, allowed_formats, is_compression_enabled,
			version, updated_by_user_id, updated_at
		FROM public.image_policies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Policy{}
	for rows.Next() {
		var p Policy
		var formats []byte
		var by sql.NullInt64
		var at sql.NullTime
		if err := rows.Scan(&p.Category, &p.MaxSide, &p.MinSide, &p.Quality, &p.MaxBytes, &formats,
			&p.IsCompressionEnabled, &p.Version, &by, &at); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(formats, &p.AllowedFormats); err != nil {
			return nil, err
		}
		// A row that breaks the bounds (edited by hand) is not used.
		if Validate(p) != nil {
			slog.Warn("image policy row outside safe bounds; using the default", "category", p.Category)
			continue
		}
		if by.Valid {
			p.UpdatedByUserID = &by.Int64
		}
		if at.Valid {
			p.UpdatedAt = &at.Time
		}
		out[p.Category] = p
	}
	return out, rows.Err()
}

// HistoryEntry is one saved version of a category's policy.
type HistoryEntry struct {
	Version     int       `json:"version"`
	Policy      Policy    `json:"policy"`
	ChangedBy   *int64    `json:"changed_by_user_id,omitempty"`
	ChangedName *string   `json:"changed_by_name,omitempty"`
	Note        *string   `json:"change_note,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Update saves p as the next version of its category when the current
// version is expectedVersion, and records it in the history. p must pass
// Validate. Images already stored are not touched: policies apply to new uploads.
func (s *Store) Update(ctx context.Context, actorID int64, p Policy, expectedVersion int, note *string) (Policy, error) {
	if err := Validate(p); err != nil {
		return Policy{}, err
	}
	formats, err := json.Marshal(p.AllowedFormats)
	if err != nil {
		return Policy{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Policy{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var policyID int64
	var version int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.image_policies (category, max_side, min_side, quality, max_bytes, allowed_formats,
			is_compression_enabled, version, updated_by_user_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, 1, $8, CURRENT_TIMESTAMP)
		ON CONFLICT (category) DO UPDATE
		SET max_side = EXCLUDED.max_side, min_side = EXCLUDED.min_side, quality = EXCLUDED.quality,
			max_bytes = EXCLUDED.max_bytes, allowed_formats = EXCLUDED.allowed_formats,
			is_compression_enabled = EXCLUDED.is_compression_enabled,
			version = public.image_policies.version + 1,
			updated_by_user_id = EXCLUDED.updated_by_user_id, updated_at = CURRENT_TIMESTAMP
		WHERE public.image_policies.version = $9
		RETURNING image_policy_id, version`,
		p.Category, p.MaxSide, p.MinSide, p.Quality, p.MaxBytes, string(formats), p.IsCompressionEnabled, actorID, expectedVersion).Scan(&policyID, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return Policy{}, ErrVersionConflict
	}
	if err != nil {
		return Policy{}, err
	}
	p.Version, p.UpdatedByUserID = version, &actorID
	snapshot, err := json.Marshal(p)
	if err != nil {
		return Policy{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.image_policy_versions (image_policy_id, category, version, policy, changed_by_user_id, change_note)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6)`, policyID, p.Category, version, string(snapshot), actorID, note); err != nil {
		return Policy{}, err
	}
	if err := tx.Commit(); err != nil {
		return Policy{}, err
	}
	s.mu.Lock()
	s.cached = nil // this instance sees the change at once
	s.mu.Unlock()
	return s.Get(ctx, p.Category), nil
}

// History lists the saved versions of category, newest first.
func (s *Store) History(ctx context.Context, category string, limit int) ([]HistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT h.version, h.policy, h.changed_by_user_id,
			NULLIF(btrim(CONCAT_WS(' ', u.first_name, u.last_name)), ''), h.change_note, h.created_at
		FROM public.image_policy_versions h
		LEFT JOIN public.users u ON u.user_id = h.changed_by_user_id
		WHERE h.category = $1
		ORDER BY h.version DESC
		LIMIT $2`, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]HistoryEntry, 0)
	for rows.Next() {
		var e HistoryEntry
		var raw []byte
		var by sql.NullInt64
		var name, note sql.NullString
		if err := rows.Scan(&e.Version, &raw, &by, &name, &note, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &e.Policy); err != nil {
			return nil, err
		}
		if by.Valid {
			e.ChangedBy = &by.Int64
		}
		if name.Valid {
			e.ChangedName = &name.String
		}
		if note.Valid {
			e.Note = &note.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
