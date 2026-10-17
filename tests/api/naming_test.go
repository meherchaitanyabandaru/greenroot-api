package apitest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestSchemaNamingConvention enforces the database naming convention
// (greenroot-docs/06-decisions/enterprise-engineering-standards.md, D13)
// against the live catalog, so a migration cannot introduce a
// non-conforming name. Every exception is listed here with its reason.
func TestSchemaNamingConvention(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres:///greenroot?host=/tmp"
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, v := range namingViolations(ctx, t, db) {
		t.Error(v)
	}
}

var (
	snake = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

	// Mass nouns read wrongly in the plural.
	singularTables = map[string]bool{"nursery_inventory": true, "platform_config": true, "vehicle_tracking": true}

	// Tables whose key is not a surrogate <entity>_id.
	naturalKeys = map[string]bool{
		"schema_migrations":       true, // version string
		"public_code_sequences":   true, // (code_key, date_key)
		"user_roles":              true, // (user_id, role_id)
		"plant_category_mappings": true, // (plant_id, category_id)
	}

	// Validity windows are named for what they bound, date or timestamp.
	validityBounds = map[string]bool{"valid_from": true, "valid_until": true}

	boolPrefix = regexp.MustCompile(`^(is|has|can|requires|allows|should|would)_`)
)

// Owned by extensions (PostGIS), not by us.
const extensionFilter = `NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')`

func namingViolations(ctx context.Context, t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string
	add := func(format string, args ...any) { out = append(out, sprintf(format, args...)) }

	// ── Tables and columns ───────────────────────────────────────────────
	rows := query(ctx, t, db, `
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod)
		FROM pg_class c
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p') AND `+extensionFilter+`
		ORDER BY 1, a.attnum`)
	tables := map[string][]string{}
	for _, r := range rows {
		table, col, typ := r[0], r[1], r[2]
		tables[table] = append(tables[table], col)
		if !snake.MatchString(col) {
			add("%s.%s: not snake_case", table, col)
		}
		switch {
		case typ == "timestamp with time zone" || typ == "timestamp without time zone":
			if !strings.HasSuffix(col, "_at") && !validityBounds[col] {
				add("%s.%s: timestamp must end in _at", table, col)
			}
		case typ == "date":
			if !strings.HasSuffix(col, "_date") && !validityBounds[col] {
				add("%s.%s: date must end in _date", table, col)
			}
		case typ == "boolean":
			if !boolPrefix.MatchString(col) {
				add("%s.%s: boolean must start with is_/has_/can_/requires_/allows_/should_/would_", table, col)
			}
		}
		if strings.HasSuffix(col, "_by") {
			add("%s.%s: actor columns are <verb>_by_user_id", table, col)
		}
	}
	for table, cols := range tables {
		if !snake.MatchString(table) {
			add("%s: table not snake_case", table)
		}
		if !strings.HasSuffix(table, "s") && !singularTables[table] {
			add("%s: table names are plural", table)
		}
		// The main lifecycle column is `status`; extra ones are <aspect>_status.
		var statuses []string
		for _, c := range cols {
			if c == "status" || strings.HasSuffix(c, "_status") {
				statuses = append(statuses, c)
			}
		}
		if len(statuses) == 1 && statuses[0] != "status" {
			add("%s.%s: a table's only status column is named status", table, statuses[0])
		}
	}

	// ── Keys ─────────────────────────────────────────────────────────────
	for _, r := range query(ctx, t, db, `
		SELECT c.relname, string_agg(a.attname, ',' ORDER BY a.attnum)
		FROM pg_constraint k
		JOIN pg_class c ON c.oid = k.conrelid
		JOIN pg_attribute a ON a.attrelid = k.conrelid AND a.attnum = ANY (k.conkey)
		WHERE k.contype = 'p' AND k.connamespace = 'public'::regnamespace AND `+extensionFilter+`
		GROUP BY c.relname`) {
		table, cols := r[0], r[1]
		if naturalKeys[table] {
			continue
		}
		if strings.Contains(cols, ",") || !strings.HasSuffix(cols, "_id") || cols == "id" {
			add("%s: primary key %q must be a single <entity>_id column", table, cols)
		}
	}
	for _, r := range query(ctx, t, db, `
		SELECT c.relname, a.attname
		FROM pg_constraint k
		JOIN pg_class c ON c.oid = k.conrelid
		JOIN pg_attribute a ON a.attrelid = k.conrelid AND a.attnum = k.conkey[1]
		WHERE k.contype = 'f' AND k.connamespace = 'public'::regnamespace AND array_length(k.conkey, 1) = 1`) {
		if !strings.HasSuffix(r[1], "_id") {
			add("%s.%s: foreign key columns end in _id", r[0], r[1])
		}
	}

	// ── Constraint and index names ───────────────────────────────────────
	for _, r := range query(ctx, t, db, `
		SELECT c.relname, k.conname, k.contype::text
		FROM pg_constraint k JOIN pg_class c ON c.oid = k.conrelid
		WHERE k.connamespace = 'public'::regnamespace AND k.contype IN ('p', 'f', 'u', 'c') AND `+extensionFilter) {
		table, name, kind := r[0], r[1], r[2]
		ok := false
		switch kind {
		case "p":
			ok = name == table+"_pkey"
		case "f":
			ok = strings.HasPrefix(name, table+"_") && strings.HasSuffix(name, "_fkey")
		case "u":
			ok = strings.HasPrefix(name, table+"_") && strings.HasSuffix(name, "_key")
		case "c":
			ok = strings.HasPrefix(name, "ck_"+table+"_")
		}
		if !ok {
			add("%s: constraint %q (%s) does not follow the naming convention", table, name, kind)
		}
	}
	for _, r := range query(ctx, t, db, `
		SELECT c.relname, i.relname
		FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_class c ON c.oid = x.indrelid
		WHERE c.relnamespace = 'public'::regnamespace AND `+extensionFilter+`
		  AND NOT EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conindid = x.indexrelid)`) {
		table, name := r[0], r[1]
		prefix := "idx_"
		if strings.HasPrefix(name, "uq_") {
			prefix = "uq_"
		}
		if !strings.HasPrefix(name, prefix+table+"_") && name != prefix+table {
			add("%s: index %q must be idx_<table>_… or uq_<table>_…", table, name)
		}
	}
	sort.Strings(out)
	return out
}

func query(ctx context.Context, t *testing.T, db *sql.DB, q string) [][]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
