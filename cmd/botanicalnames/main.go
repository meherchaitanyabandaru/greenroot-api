// Command botanicalnames prepares and loads the World Flora Online (WFO)
// taxonomic backbone into public.botanical_names.
//
//	go run ./cmd/botanicalnames clean [-in classification.csv] [-out botanical_names.csv]
//	go run ./cmd/botanicalnames load  [-in botanical_names.csv] [-batch 5000] [-restart]
//	go run ./cmd/botanicalnames link
//
// clean keeps the 8 columns GreenRoot uses from the WFO download (tab
// separated, ~1.66 M rows) and writes a comma-separated seed file. load
// upserts that file in batches; each batch and its checkpoint commit in one
// transaction, so an interrupted load resumes exactly where it stopped.
// Re-running a finished load does nothing; -restart reloads (upserts) from row 1,
// e.g. after downloading a newer WFO release. After loading it runs link,
// which connects catalog plants entered by hand (public.plants with no
// wfo_id) to their botanical name when the scientific name is unambiguous,
// so the pickers never offer a second copy of a species. Uses DATABASE_URL.
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database"
)

const (
	defaultSource = "../greenroot-infra/classification.csv"
	defaultSeed   = "../greenroot-infra/db/seed-data/botanical_names.csv"
)

// seedHeader is the cleaned file's column order; load relies on it.
var seedHeader = []string{"wfo_id", "scientific_name", "authorship", "taxon_rank", "family", "genus", "status", "accepted_wfo_id"}

// wfoColumns are the source columns for seedHeader, in the same order.
var wfoColumns = []string{"taxonID", "scientificName", "scientificNameAuthorship", "taxonRank", "family", "genus", "taxonomicStatus", "acceptedNameUsageID"}

var statuses = map[string]string{"Accepted": "ACCEPTED", "Synonym": "SYNONYM", "Unchecked": "UNCHECKED"}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "botanicalnames:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: botanicalnames clean | load  (-h for flags)")
	}
	fs := flag.NewFlagSet(args[0], flag.ExitOnError)
	switch args[0] {
	case "clean":
		in := fs.String("in", defaultSource, "WFO classification file (tab separated)")
		out := fs.String("out", defaultSeed, "cleaned seed CSV to write")
		_ = fs.Parse(args[1:])
		n, err := cleanFile(*in, *out)
		if err != nil {
			return err
		}
		fmt.Printf("wrote %d rows to %s\n", n, *out)
		return nil
	case "load":
		in := fs.String("in", defaultSeed, "cleaned seed CSV (from clean)")
		batch := fs.Int("batch", 5000, "rows per transaction")
		restart := fs.Bool("restart", false, "ignore the checkpoint and load from row 1")
		_ = fs.Parse(args[1:])
		if *batch < 1 {
			return errors.New("-batch must be at least 1")
		}
		return load(*in, *batch, *restart)
	case "link":
		_ = fs.Parse(args[1:])
		return withDB(func(ctx context.Context, db *sql.DB) error { return linkCatalog(ctx, db) })
	}
	return fmt.Errorf("unknown command %q (want clean, load or link)", args[0])
}

// cleanFile writes to a temp file and renames it at the end, so a half-written
// seed is never mistaken for a complete one.
func cleanFile(inPath, outPath string) (int, error) {
	in, err := os.Open(inPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".botanical_names-*.csv")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	n, err := clean(in, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil { // CreateTemp makes it owner-only
		return n, err
	}
	return n, os.Rename(tmp.Name(), outPath)
}

func clean(r io.Reader, w io.Writer) (int, error) {
	cr := csv.NewReader(r)
	cr.Comma = '\t'
	cr.LazyQuotes = true
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return 0, fmt.Errorf("read header: %w", err)
	}
	idx := make([]int, len(wfoColumns))
	for i, name := range wfoColumns {
		idx[i] = indexOf(header, name)
		if idx[i] < 0 {
			return 0, fmt.Errorf("source has no %q column; is this the WFO classification file?", name)
		}
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(seedHeader); err != nil {
		return 0, err
	}
	seen := make(map[string]struct{}, 1<<21)
	out := make([]string, len(seedHeader))
	n := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		line, _ := cr.FieldPos(0)
		if err != nil {
			return n, err
		}
		for i, j := range idx {
			// The WFO file has a few non-UTF-8 bytes; PostgreSQL rejects them.
			out[i] = strings.ToValidUTF8(strings.TrimSpace(rec[j]), "�")
		}
		status, ok := statuses[out[6]]
		if !ok {
			return n, fmt.Errorf("line %d: unknown taxonomicStatus %q", line, out[6])
		}
		out[6] = status
		if out[0] == "" || out[1] == "" || out[3] == "" {
			return n, fmt.Errorf("line %d: taxonID, scientificName and taxonRank are required", line)
		}
		if (status == "SYNONYM") != (out[7] != "") {
			return n, fmt.Errorf("line %d: only a synonym has an accepted name (status %s, accepted %q)", line, status, out[7])
		}
		if _, dup := seen[out[0]]; dup {
			return n, fmt.Errorf("line %d: duplicate taxonID %s", line, out[0])
		}
		seen[out[0]] = struct{}{}
		if err := cw.Write(out); err != nil {
			return n, err
		}
		n++
	}
	cw.Flush()
	return n, cw.Error()
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

// Arrays keep the statement fixed at 8 parameters whatever the batch size.
const upsertNames = `
	INSERT INTO public.botanical_names
		(wfo_id, scientific_name, authorship, taxon_rank, family, genus, status, accepted_wfo_id)
	SELECT w, s, NULLIF(a, ''), r, NULLIF(f, ''), NULLIF(g, ''), st, NULLIF(acc, '')
	FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[])
		AS t(w, s, a, r, f, g, st, acc)
	ON CONFLICT (wfo_id) DO UPDATE SET
		scientific_name = EXCLUDED.scientific_name,
		authorship      = EXCLUDED.authorship,
		taxon_rank      = EXCLUDED.taxon_rank,
		family          = EXCLUDED.family,
		genus           = EXCLUDED.genus,
		status          = EXCLUDED.status,
		accepted_wfo_id = EXCLUDED.accepted_wfo_id`

const saveCheckpoint = `
	INSERT INTO public.import_checkpoints (source_name, rows_done) VALUES ($1, $2)
	ON CONFLICT (source_name) DO UPDATE SET rows_done = EXCLUDED.rows_done, updated_at = now()`

const readCheckpoint = `SELECT rows_done FROM public.import_checkpoints WHERE source_name = $1`

func withDB(fn func(context.Context, *sql.DB) error) error {
	cfg := config.Load()
	if cfg.Database.URL == "" {
		return errors.New("DATABASE_URL is required")
	}
	// Ctrl-C or a container stop rolls back only the transaction in flight.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(ctx, db)
}

func load(path string, batchSize int, restart bool) error {
	return withDB(func(ctx context.Context, db *sql.DB) error {
		if err := loadFile(ctx, db, path, batchSize, restart); err != nil {
			return err
		}
		return linkCatalog(ctx, db)
	})
}

func loadFile(ctx context.Context, db *sql.DB, path string, batchSize int, restart bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	if strings.Join(header, ",") != strings.Join(seedHeader, ",") {
		return fmt.Errorf("%s is not a cleaned seed file (run clean first); header %v", path, header)
	}

	source := "botanical_names:" + filepath.Base(path)
	var done int64
	if !restart {
		err := db.QueryRowContext(ctx, readCheckpoint, source).Scan(&done)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read checkpoint: %w", err)
		}
	}
	if done > 0 {
		fmt.Printf("resuming after row %d\n", done)
	}

	var row int64
	for row < done { // skip rows a previous run committed
		if _, err := cr.Read(); err == io.EOF {
			return fmt.Errorf("checkpoint says %d rows done but %s has only %d; a different file? use -restart", done, path, row)
		} else if err != nil {
			return err
		}
		row++
	}

	cols := make([][]string, len(seedHeader))
	for {
		for i := range cols {
			cols[i] = cols[i][:0]
		}
		for len(cols[0]) < batchSize {
			rec, err := cr.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			for i := range cols {
				cols[i] = append(cols[i], rec[i])
			}
		}
		if len(cols[0]) == 0 {
			break
		}
		row += int64(len(cols[0]))
		if err := commitBatch(ctx, db, cols, source, row); err != nil {
			return fmt.Errorf("batch ending at row %d (rerun to resume from row %d): %w", row, row-int64(len(cols[0])), err)
		}
		fmt.Printf("loaded %d rows\n", row)
	}
	if err := commitCheckpointOnly(ctx, db, source, row); err != nil {
		return err
	}
	fmt.Printf("done: %d rows in public.botanical_names from %s\n", row, path)
	return nil
}

func commitBatch(ctx context.Context, db *sql.DB, cols [][]string, source string, rowsDone int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	args := make([]any, len(cols))
	for i := range cols {
		args[i] = cols[i]
	}
	if _, err := tx.ExecContext(ctx, upsertNames, args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, saveCheckpoint, source, rowsDone); err != nil {
		return err
	}
	return tx.Commit()
}

// linkCatalogPlants sets plants.wfo_id (to the accepted species) for unlinked
// plants whose scientific name means exactly one species. Ambiguous names
// (e.g. "Rosa indica", used by several authors for different species) and
// species already linked to another plant are left for an admin.
const linkCatalogPlants = `
	WITH unique_names AS (
		SELECT lower(scientific_name) AS name_key, min(COALESCE(accepted_wfo_id, wfo_id)) AS species
		FROM public.botanical_names
		WHERE status IN ('ACCEPTED', 'SYNONYM')
		GROUP BY lower(scientific_name)
		HAVING count(DISTINCT COALESCE(accepted_wfo_id, wfo_id)) = 1
	),
	linked AS (
		SELECT COALESCE(b.accepted_wfo_id, p.wfo_id) AS species
		FROM public.plants p
		LEFT JOIN public.botanical_names b ON b.wfo_id = p.wfo_id
		WHERE p.wfo_id IS NOT NULL
	),
	candidates AS (
		SELECT DISTINCT ON (u.species) p.plant_id, u.species
		FROM public.plants p
		JOIN unique_names u ON u.name_key = lower(p.scientific_name)
		WHERE p.wfo_id IS NULL AND u.species NOT IN (SELECT species FROM linked)
		ORDER BY u.species, p.is_active DESC, p.plant_id
	)
	UPDATE public.plants p SET wfo_id = c.species, updated_at = CURRENT_TIMESTAMP
	FROM candidates c
	WHERE p.plant_id = c.plant_id`

func linkCatalog(ctx context.Context, db *sql.DB) error {
	res, err := db.ExecContext(ctx, linkCatalogPlants)
	if err != nil {
		return fmt.Errorf("link catalog plants: %w", err)
	}
	n, _ := res.RowsAffected()
	var unlinked int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.plants WHERE wfo_id IS NULL AND is_active`).Scan(&unlinked); err != nil {
		return err
	}
	fmt.Printf("linked %d catalog plants to botanical names; %d active plants still unlinked (fix in Admin → Plants)\n", n, unlinked)
	return nil
}

// commitCheckpointOnly records completion for an empty file or a -restart
// of a file whose rows were all skipped.
func commitCheckpointOnly(ctx context.Context, db *sql.DB, source string, rowsDone int64) error {
	_, err := db.ExecContext(ctx, saveCheckpoint, source, rowsDone)
	return err
}
