package database

import (
	"context"
	"database/sql"
)

// PrivilegeProblems reports why the connected role is more powerful than an
// application role should be (standard D9): superuser, BYPASSRLS, schema
// owner or able to create objects. The API refuses a production start when
// this is non-empty; see greenroot-infra/db/postgresql/roles.sql.
func PrivilegeProblems(ctx context.Context, db *sql.DB) ([]string, error) {
	var (
		role                    string
		super, bypassRLS        bool
		createInPublic, ownsAny bool
	)
	err := db.QueryRowContext(ctx, `
		SELECT r.rolname, r.rolsuper, r.rolbypassrls,
		       has_schema_privilege('public', 'CREATE'),
		       EXISTS (SELECT 1 FROM pg_class c
		               WHERE c.relnamespace = 'public'::regnamespace
		                 AND pg_has_role(c.relowner, 'USAGE'))
		FROM pg_roles r WHERE r.rolname = current_user`,
	).Scan(&role, &super, &bypassRLS, &createInPublic, &ownsAny)
	if err != nil {
		return nil, err
	}
	var problems []string
	if super {
		problems = append(problems, role+" is a superuser")
	}
	if bypassRLS {
		problems = append(problems, role+" bypasses row-level security")
	}
	if ownsAny {
		problems = append(problems, role+" owns (or is a member of the owner of) schema objects, so it can ALTER or DROP them")
	}
	if createInPublic {
		problems = append(problems, role+" can create objects in schema public")
	}
	return problems, nil
}
