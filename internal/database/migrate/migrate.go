// Package migrate applies the SQL files in internal/database/migrations to a
// database and records them in public.schema_migrations.
//
// File naming: <version>_<description>.up.sql (+ optional .down.sql, kept for
// manual rollback). Versions sort lexically, so use YYYYMMDD[_NN].
// A file containing the line "-- migrate:no-transaction" runs outside a
// transaction, one statement at a time (needed for CREATE INDEX CONCURRENTLY).
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

const (
	noTxDirective = "-- migrate:no-transaction"
	upSuffix      = ".up.sql"
)

type Migration struct {
	Version  string // file name without ".up.sql"
	SQL      string
	Checksum string
	NoTx     bool
}

type Applied struct {
	Version   string
	Checksum  string
	AppliedAt time.Time
}

// Load reads every *.up.sql in fsys, sorted by version.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, upSuffix) {
			continue
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		out = append(out, Migration{
			Version:  strings.TrimSuffix(name, upSuffix),
			SQL:      string(body),
			Checksum: hex.EncodeToString(sum[:]),
			NoTx:     strings.Contains(string(body), noTxDirective),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

type Runner struct {
	db         *sql.DB
	migrations []Migration
}

func New(db *sql.DB, migrations []Migration) *Runner { return &Runner{db: db, migrations: migrations} }

// ensureTable creates schema_migrations, or adopts the (version, applied_at)
// table left by the API's original runner by adding the checksum column; its
// existing rows count as baselined.
func (r *Runner) ensureTable(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
		)`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		ALTER TABLE public.schema_migrations
		ADD COLUMN IF NOT EXISTS checksum character varying(64) DEFAULT '`+baselineChecksum+`' NOT NULL`)
	return err
}

func (r *Runner) applied(ctx context.Context) (map[string]Applied, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT version, checksum, applied_at FROM public.schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Applied{}
	for rows.Next() {
		var a Applied
		if err := rows.Scan(&a.Version, &a.Checksum, &a.AppliedAt); err != nil {
			return nil, err
		}
		out[a.Version] = a
	}
	return out, rows.Err()
}

// Pending lists migrations not yet applied. It also fails if an applied
// migration's file changed since it ran: edit a new migration instead.
func (r *Runner) Pending(ctx context.Context) ([]Migration, error) {
	if err := r.ensureTable(ctx); err != nil {
		return nil, err
	}
	done, err := r.applied(ctx)
	if err != nil {
		return nil, err
	}
	return r.pendingFrom(done)
}

func (r *Runner) pendingFrom(done map[string]Applied) ([]Migration, error) {
	var pending []Migration
	for _, m := range r.migrations {
		a, ok := done[m.Version]
		if !ok {
			pending = append(pending, m)
			continue
		}
		if a.Checksum != m.Checksum && a.Checksum != baselineChecksum {
			return nil, fmt.Errorf("migration %s was modified after it was applied; add a new migration instead", m.Version)
		}
	}
	return pending, nil
}

// Up applies every pending migration in order and returns their versions.
func (r *Runner) Up(ctx context.Context) ([]string, error) {
	pending, err := r.Pending(ctx)
	if err != nil {
		return nil, err
	}
	var done []string
	for _, m := range pending {
		if err := r.apply(ctx, m); err != nil {
			return done, fmt.Errorf("migration %s: %w", m.Version, err)
		}
		done = append(done, m.Version)
	}
	return done, nil
}

func (r *Runner) apply(ctx context.Context, m Migration) error {
	record := `INSERT INTO public.schema_migrations (version, checksum) VALUES ($1, $2)`
	if m.NoTx {
		// One statement per Exec: a multi-statement string would run in an
		// implicit transaction, which CONCURRENTLY rejects.
		for _, stmt := range splitStatements(m.SQL) {
			if _, err := r.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		_, err := r.db.ExecContext(ctx, record, m.Version, m.Checksum)
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, record, m.Version, m.Checksum); err != nil {
		return err
	}
	return tx.Commit()
}

// baselineChecksum marks versions recorded by Baseline rather than run, so a
// later edit to those historical files is not reported as tampering.
const baselineChecksum = "baseline"

// Baseline records every migration up to and including version as applied
// without running it — for databases built from schema.sql or migrated by
// hand before this runner existed.
func (r *Runner) Baseline(ctx context.Context, version string) ([]string, error) {
	if err := r.ensureTable(ctx); err != nil {
		return nil, err
	}
	found := false
	for _, m := range r.migrations {
		if m.Version == version {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown migration version %q", version)
	}
	var marked []string
	for _, m := range r.migrations {
		if m.Version > version {
			break
		}
		res, err := r.db.ExecContext(ctx, `INSERT INTO public.schema_migrations (version, checksum) VALUES ($1, $2) ON CONFLICT (version) DO NOTHING`, m.Version, baselineChecksum)
		if err != nil {
			return marked, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			marked = append(marked, m.Version)
		}
	}
	return marked, nil
}

// Status reports each migration and whether it is applied.
func (r *Runner) Status(ctx context.Context) ([]StatusLine, error) {
	if err := r.ensureTable(ctx); err != nil {
		return nil, err
	}
	done, err := r.applied(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StatusLine, 0, len(r.migrations))
	for _, m := range r.migrations {
		line := StatusLine{Version: m.Version}
		if a, ok := done[m.Version]; ok {
			line.Applied, line.AppliedAt, line.Baselined = true, a.AppliedAt, a.Checksum == baselineChecksum
		}
		out = append(out, line)
	}
	return out, nil
}

type StatusLine struct {
	Version   string
	Applied   bool
	Baselined bool
	AppliedAt time.Time
}

// splitStatements splits on ";" at line ends and drops comment-only chunks.
// Only used for no-transaction files, which must be plain DDL statements.
func splitStatements(sqlText string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, `\`) {
			continue
		}
		cur.WriteString(line + "\n")
		if strings.HasSuffix(trimmed, ";") {
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// ErrPending is returned by CheckCurrent when the schema is behind the code.
var ErrPending = errors.New("database has pending migrations")

// CheckCurrent fails when migrations are pending; the API calls it at startup.
// It only reads: the API's database role has no DDL rights, so a ledger that
// does not exist yet (or predates the checksum column) means "run migrate up".
func (r *Runner) CheckCurrent(ctx context.Context) ([]Migration, error) {
	var ready bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'schema_migrations' AND column_name = 'checksum')`,
	).Scan(&ready); err != nil {
		return nil, err
	}
	if !ready {
		return r.migrations, ErrPending
	}
	done, err := r.applied(ctx)
	if err != nil {
		return nil, err
	}
	pending, err := r.pendingFrom(done)
	if err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		return pending, ErrPending
	}
	return nil, nil
}
