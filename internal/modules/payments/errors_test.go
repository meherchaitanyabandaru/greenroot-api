package payments

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

// A duplicate gateway payment (uq_payments_provider_payment) and a rejected
// amount/target (ck_payments_*) reach the client as 409 / 400, not 500.
func TestWritePaymentsError_DatabaseConstraints(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"duplicate provider payment", pgerr.Map(&pgconn.PgError{Code: pgerr.UniqueViolation, ConstraintName: "uq_payments_provider_payment"}), http.StatusConflict},
		{"amount check", pgerr.Map(&pgconn.PgError{Code: pgerr.CheckViolation, ConstraintName: "ck_payments_amount_positive"}), http.StatusBadRequest},
		{"target check", pgerr.Map(&pgconn.PgError{Code: pgerr.CheckViolation, ConstraintName: "ck_payments_target"}), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writePaymentsError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
