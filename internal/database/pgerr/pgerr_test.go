package pgerr

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

func TestMap(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"unique", &pgconn.PgError{Code: UniqueViolation, ConstraintName: "uq_x"}, apperrs.ErrConflict},
		{"check", &pgconn.PgError{Code: CheckViolation, ConstraintName: "ck_x"}, apperrs.ErrInvalidInput},
		{"foreign key", &pgconn.PgError{Code: ForeignKeyViolation, ConstraintName: "fk_x"}, apperrs.ErrInvalidInput},
		{"not null", &pgconn.PgError{Code: NotNullViolation, ColumnName: "amount"}, apperrs.ErrInvalidInput},
		{"wrapped", fmt.Errorf("insert: %w", &pgconn.PgError{Code: UniqueViolation}), apperrs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Map(tc.in); !errors.Is(got, tc.want) {
				t.Fatalf("Map = %v, want %v", got, tc.want)
			}
		})
	}

	other := &pgconn.PgError{Code: "40001"}
	if got := Map(other); got != error(other) {
		t.Fatalf("serialization failure must pass through, got %v", got)
	}
	plain := errors.New("boom")
	if Map(plain) != plain || Map(nil) != nil {
		t.Fatal("non-pg errors must pass through unchanged")
	}
	if Constraint(fmt.Errorf("x: %w", &pgconn.PgError{ConstraintName: "ck_y"})) != "ck_y" {
		t.Fatal("Constraint did not unwrap")
	}
}
