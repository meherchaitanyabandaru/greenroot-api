// Package migrations embeds the SQL migrations so the API binary and
// cmd/migrate carry them; see internal/database/migrate for the runner.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
