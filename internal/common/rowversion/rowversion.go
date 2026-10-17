// Package rowversion implements optimistic concurrency (standard D5) over
// the `version` column that every core aggregate carries.
//
// A client that read version N sends `If-Match: N` (or `"N"`) on PUT/PATCH.
// The repository adds `AND ($k::bigint IS NULL OR version = $k)` with Arg(ctx)
// as $k; zero rows on an existing record means someone else changed it, and
// the API answers 409 version_conflict. Without If-Match the write is
// unconditional, so existing clients keep working.
package rowversion

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

type ctxKey struct{}

// ErrMalformed is returned for an If-Match value that is not a version.
var ErrMalformed = errors.New("If-Match must be a row version, e.g. If-Match: 3")

// Parse reads an If-Match header value: `3`, `"3"` or `W/"3"`. An empty value
// or `*` means "no precondition".
func Parse(header string) (int64, bool, error) {
	v := strings.TrimSpace(header)
	if v == "" || v == "*" {
		return 0, false, nil
	}
	v = strings.TrimPrefix(v, "W/")
	v = strings.Trim(v, `"`)
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return 0, false, ErrMalformed
	}
	return n, true, nil
}

// Middleware stores the If-Match version in the request context and rejects
// a malformed header with 400 before any handler runs.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, ok, err := Parse(r.Header.Get("If-Match"))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_if_match", err.Error())
			return
		}
		if ok {
			r = r.WithContext(With(r.Context(), n))
		}
		next.ServeHTTP(w, r)
	})
}

// With returns ctx carrying the expected version (used by tests and callers
// that take the version from a request body).
func With(ctx context.Context, version int64) context.Context {
	return context.WithValue(ctx, ctxKey{}, version)
}

// Expected returns the version the client expects, if it sent one.
func Expected(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(ctxKey{}).(int64)
	return v, ok
}

// Arg is the SQL argument for `($k::bigint IS NULL OR version = $k)`:
// the expected version, or nil when the client sent no precondition.
func Arg(ctx context.Context) any {
	if v, ok := Expected(ctx); ok {
		return v
	}
	return nil
}
