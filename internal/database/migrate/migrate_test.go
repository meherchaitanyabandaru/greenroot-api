package migrate

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/migrations"
)

func TestLoad_OrderAndDirectives(t *testing.T) {
	fsys := fstest.MapFS{
		"20261016_02_b.up.sql":   {Data: []byte("-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY x ON t (a);\n")},
		"20261016_01_a.up.sql":   {Data: []byte("CREATE TABLE t (a int);")},
		"20261016_01_a.down.sql": {Data: []byte("DROP TABLE t;")},
		"README.md":              {Data: []byte("not sql")},
	}
	ms, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Version != "20261016_01_a" || ms[1].Version != "20261016_02_b" {
		t.Fatalf("migrations = %+v", ms)
	}
	if ms[0].NoTx || !ms[1].NoTx {
		t.Fatal("no-transaction directive not detected correctly")
	}
	if ms[0].Checksum == "" || ms[0].Checksum == ms[1].Checksum {
		t.Fatal("checksums must be set and differ per file")
	}
}

func TestSplitStatements(t *testing.T) {
	got := splitStatements(`-- header comment
\set ON_ERROR_STOP on

-- one
CREATE INDEX CONCURRENTLY IF NOT EXISTS a
    ON t USING btree (x);

CREATE INDEX CONCURRENTLY IF NOT EXISTS b ON t (y);
`)
	if len(got) != 2 || !strings.HasPrefix(got[0], "CREATE INDEX CONCURRENTLY IF NOT EXISTS a") || !strings.HasSuffix(got[1], "(y);") {
		t.Fatalf("statements = %q", got)
	}
}

// The real migration set must load, be ordered, and keep the concurrent
// index build out of a transaction.
func TestEmbeddedMigrations(t *testing.T) {
	ms, err := Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) < 6 {
		t.Fatalf("expected the embedded migrations, got %d", len(ms))
	}
	for i := 1; i < len(ms); i++ {
		if ms[i-1].Version >= ms[i].Version {
			t.Fatalf("out of order: %s before %s", ms[i-1].Version, ms[i].Version)
		}
	}
	for _, m := range ms {
		if strings.Contains(m.SQL, "CONCURRENTLY") && !m.NoTx {
			t.Errorf("%s uses CONCURRENTLY but is not marked -- migrate:no-transaction", m.Version)
		}
		if strings.Contains(m.SQL, `\set`) || strings.Contains(strings.ToUpper(m.SQL), "\nBEGIN;") {
			t.Errorf("%s contains psql meta-commands or its own transaction; the runner handles both", m.Version)
		}
	}
}
