// Package migrations embeds the SQL migrations so the API binary and
// cmd/migrate carry them; see internal/database/migrate for the runner.
package migrations

import "embed"

// "*" rather than "*.sql": before launch the folder holds no migrations
// (greenroot-infra/db/postgresql/schema.sql is the schema), and an embed
// pattern that matches nothing does not compile. Load only reads *.up.sql.
//
//go:embed *
var FS embed.FS
