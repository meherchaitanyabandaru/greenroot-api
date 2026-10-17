package apitest

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/database"
)

// TestDatabaseRoles checks least privilege (standard D9) when the API's role
// is provided: the app role has no admin powers, while the fixture role
// (DATABASE_URL, a superuser in CI) is correctly reported as over-privileged.
func TestDatabaseRoles(t *testing.T) {
	appURL := os.Getenv("APP_DATABASE_URL")
	if appURL == "" {
		t.Skip("APP_DATABASE_URL not set (roles.sql not applied in this environment)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	problems := func(url string) []string {
		t.Helper()
		db, err := sql.Open("pgx", url)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		p, err := database.PrivilegeProblems(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	if p := problems(appURL); len(p) != 0 {
		t.Fatalf("greenroot_app is over-privileged: %v", p)
	}
	if p := problems(os.Getenv("DATABASE_URL")); len(p) == 0 {
		t.Fatal("superuser fixture role should be reported as over-privileged")
	}

	// The app role can read and write rows but not change the schema.
	db, err := sql.Open("pgx", appURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `SELECT count(*) FROM public.orders`); err != nil {
		t.Fatalf("app role cannot read orders: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE public.should_not_exist (id int)`,
		`ALTER TABLE public.orders ADD COLUMN should_not_exist int`,
		`DELETE FROM public.schema_migrations`,
		`TRUNCATE public.audit_logs`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err == nil {
			t.Errorf("app role was allowed: %s", ddl)
		}
	}
}
