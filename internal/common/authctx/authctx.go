package authctx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

const gracePeriodDays = 7

// SubLevel is the subscription enforcement level derived from the actor's JWT claims.
type SubLevel int

const (
	SubActive  SubLevel = iota // full access — subscription valid
	SubGrace                   // within 7-day grace — reads ok, writes allowed with warning header
	SubExpired                 // grace ended — writes blocked (402)
	SubNone                    // no nursery / no subscription (BUYER, DRIVER, new user)
)

// Actor is the authenticated user context available in every handler.
// All fields are populated from JWT claims — zero DB queries per request.
type Actor struct {
	UserID    int64
	Mobile    string
	Roles     []string
	IPAddress string
	UserAgent string

	UserStatus    string // ACTIVE | SUSPENDED | DELETED
	NurseryID     int64  // 0 = not nursery-affiliated
	NurseryStatus string // ACTIVE | SUSPENDED | PENDING_APPROVAL
	SubTier       string // TRIAL | GROWTH | ENTERPRISE | ""
	SubExpEpoch   int64  // Unix epoch of subscription end_date; 0 = no expiry

	TokenJTI      string
	TokenExpEpoch int64
}

// SubLevel returns the subscription enforcement level for this actor.
// Pure arithmetic — no DB, no I/O.
func (a Actor) SubLevel() SubLevel {
	if a.NurseryID == 0 {
		return SubNone // buyer / driver — not subscription-gated
	}
	if a.SubTier == "" {
		return SubNone
	}
	if a.SubExpEpoch == 0 {
		return SubActive // no expiry (e.g. lifetime plan)
	}
	now := time.Now().Unix()
	if now <= a.SubExpEpoch {
		return SubActive
	}
	if now <= a.SubExpEpoch+int64(gracePeriodDays*24*60*60) {
		return SubGrace
	}
	return SubExpired
}

// HasRole reports whether the actor has any of the given roles.
func (a Actor) HasRole(roles ...string) bool {
	for _, r := range a.Roles {
		for _, want := range roles {
			if strings.EqualFold(r, want) {
				return true
			}
		}
	}
	return false
}

type contextKey struct{}

func StoreActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, contextKey{}, actor)
}

// ActorFromContext retrieves the actor stored by EnrichActorMiddleware.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(contextKey{}).(Actor)
	return actor, ok && actor.UserID > 0
}

// FromRequest extracts the actor from the request context (set by middleware).
// If middleware isn't present it falls back to parsing the JWT directly (for public endpoints that optionally auth).
func FromRequest(w http.ResponseWriter, r *http.Request, jwt *jwtplatform.Service) (Actor, bool) {
	if actor, ok := ActorFromContext(r.Context()); ok {
		return actor, true
	}
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
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return actorFromClaims(claims, host, r.UserAgent()), true
}

func BearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}

func actorFromClaims(c *jwtplatform.Claims, ip, ua string) Actor {
	userID, _ := strconv.ParseInt(c.UserID, 10, 64)
	return Actor{
		UserID:        userID,
		Mobile:        c.Mobile,
		Roles:         c.Roles,
		IPAddress:     ip,
		UserAgent:     ua,
		UserStatus:    c.UserStatus,
		NurseryID:     c.NurseryID,
		NurseryStatus: c.NurseryStatus,
		SubTier:       c.SubTier,
		SubExpEpoch:   c.SubExpiresEpoch,
		TokenJTI:      c.ID,
		TokenExpEpoch: tokenExpEpoch(c),
	}
}

func tokenExpEpoch(c *jwtplatform.Claims) int64 {
	if c.ExpiresAt == nil {
		return 0
	}
	return c.ExpiresAt.Time.Unix()
}

// EnrichActorMiddleware validates the JWT, checks revocation, enforces user/nursery
// status — all from in-memory data. Zero DB queries per request.
func EnrichActorMiddleware(jwt *jwtplatform.Service, redisClients ...redis.Cmdable) func(http.Handler) http.Handler {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			claims, err := jwt.VerifyAccessToken(token)
			if err != nil {
				// Let individual handlers return 401 — public routes pass through.
				next.ServeHTTP(w, r)
				return
			}

			if redisutil.IsBlocklisted(r.Context(), rdb, slog.Default(), claims.ID) {
				response.Error(w, http.StatusForbidden, "token_revoked", "token has been revoked")
				return
			}

			userID, err := strconv.ParseInt(claims.UserID, 10, 64)
			if err != nil || userID <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Revocation check — catches immediate suspend/block after token issue.
			if revocation.IsRevoked(userID) {
				response.Error(w, http.StatusForbidden, "account_suspended",
					"your account has been suspended — contact support")
				return
			}

			// User status from claims — valid for up to 15 min (access token TTL).
			status := claims.UserStatus
			if status == "" {
				status = "ACTIVE"
			}
			if status == "SUSPENDED" || status == "DELETED" {
				response.Error(w, http.StatusForbidden, "account_suspended",
					"your account has been suspended — contact support")
				return
			}

			// Nursery status — suspended nursery blocks all nursery-scoped writes.
			// Handlers that need to enforce this call actor.NurseryStatus directly.
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			actor := actorFromClaims(claims, host, r.UserAgent())
			next.ServeHTTP(w, r.WithContext(StoreActor(r.Context(), actor)))
		})
	}
}
