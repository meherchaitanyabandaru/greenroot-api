package authctx

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

// Actor is the authenticated user context shared by protected modules.
type Actor struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}

type contextKey struct{}

// StoreActor stores a pre-verified actor in the request context (used by EnrichActorMiddleware).
func StoreActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, contextKey{}, actor)
}

func FromRequest(w http.ResponseWriter, r *http.Request, jwt *jwtplatform.Service) (Actor, bool) {
	// Use actor pre-populated by EnrichActorMiddleware (roles are fresh from DB).
	if actor, ok := r.Context().Value(contextKey{}).(Actor); ok && actor.UserID > 0 {
		return actor, true
	}
	// Fallback: parse JWT directly (no middleware present).
	token := BearerToken(r)
	if token == "" {
		response.Error(w, http.StatusUnauthorized, "missing_token", "missing bearer token")
		return Actor{}, false
	}
	claims, err := jwt.VerifyAccessToken(token)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "invalid_token", "invalid access token")
		return Actor{}, false
	}
	userID, err := strconv.ParseInt(claims.UserID, 10, 64)
	if err != nil || userID <= 0 {
		response.Error(w, http.StatusUnauthorized, "invalid_token", "invalid access token")
		return Actor{}, false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return Actor{UserID: userID, Roles: claims.Roles, IPAddress: host, UserAgent: r.UserAgent()}, true
}

func BearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}
