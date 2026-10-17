// Package pgerr maps PostgreSQL constraint violations to the application's
// error sentinels, so a rule the database enforces surfaces as a 4xx with
// the constraint name instead of a 500.
package pgerr

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

// SQLSTATE codes (https://www.postgresql.org/docs/current/errcodes-appendix.html).
const (
	UniqueViolation     = "23505"
	CheckViolation      = "23514"
	ForeignKeyViolation = "23503"
	NotNullViolation    = "23502"
)

// Map returns err wrapped in apperrs.ErrConflict (unique) or
// apperrs.ErrInvalidInput (check, foreign key, not null). Any other error,
// including nil, is returned unchanged.
func Map(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case UniqueViolation:
		return fmt.Errorf("%w: %s", apperrs.ErrConflict, pgErr.ConstraintName)
	case CheckViolation, ForeignKeyViolation, NotNullViolation:
		return fmt.Errorf("%w: %s", apperrs.ErrInvalidInput, constraintOrColumn(pgErr))
	}
	return err
}

// Constraint returns the violated constraint name, or "" if err is not a
// PostgreSQL constraint error.
func Constraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

func constraintOrColumn(e *pgconn.PgError) string {
	if e.ConstraintName != "" {
		return e.ConstraintName
	}
	return e.ColumnName
}
