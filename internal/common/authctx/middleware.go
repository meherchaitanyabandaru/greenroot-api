package authctx

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"strconv"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

// RoleFetcher retrieves a user's current roles from the authoritative store.
type RoleFetcher interface {
	GetUserRoles(ctx context.Context, userID int64) ([]string, error)
}

type dbRoleFetcher struct {
	db *sql.DB
}

// NewDBRoleFetcher returns a RoleFetcher backed by the user_roles table.
func NewDBRoleFetcher(db *sql.DB) RoleFetcher {
	return &dbRoleFetcher{db: db}
}

func (f *dbRoleFetcher) GetUserRoles(ctx context.Context, userID int64) ([]string, error) {
	const query = `
		SELECT r.role_code
		FROM public.user_roles ur
		JOIN public.roles r ON r.role_id = ur.role_id
		WHERE ur.user_id = $1 AND COALESCE(r.is_active, true) = true
		ORDER BY r.role_code
	`
	rows, err := f.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]string, 0)
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// EnrichActorMiddleware verifies the JWT for identity and replaces JWT-baked roles
// with a live DB lookup on every request. Requests without a Bearer token pass through
// unchanged so public endpoints continue to work. If the DB lookup fails, the request
// is rejected with 503 (fail-closed — stale roles must never be used).
func EnrichActorMiddleware(jwt *jwtplatform.Service, rf RoleFetcher) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			claims, err := jwt.VerifyAccessToken(token)
			if err != nil {
				// Invalid/expired token: pass through so individual handlers return 401.
				next.ServeHTTP(w, r)
				return
			}
			userID, err := strconv.ParseInt(claims.UserID, 10, 64)
			if err != nil || userID <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			roles, err := rf.GetUserRoles(r.Context(), userID)
			if err != nil {
				response.Error(w, http.StatusServiceUnavailable, "service_unavailable", "unable to verify permissions")
				return
			}
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			actor := Actor{UserID: userID, Roles: roles, IPAddress: host, UserAgent: r.UserAgent()}
			next.ServeHTTP(w, r.WithContext(StoreActor(r.Context(), actor)))
		})
	}
}
