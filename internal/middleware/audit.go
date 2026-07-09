package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

// AuditContext enriches every request context with:
//   - Request ID   (from chi's RequestID middleware, propagated to X-Request-Id)
//   - Nursery ID   (from the JWT actor, when present)
//
// This must run after chi middleware.RequestID and after authctx.EnrichActorMiddleware.
// It never fails a request; if no actor is present the nursery is simply 0.
func AuditContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Propagate chi's request ID so all audit rows for one request share it.
		if rid := middleware.GetReqID(ctx); rid != "" {
			ctx = auditlog.WithRequestID(ctx, rid)
		}

		// Propagate nursery ID so modules don't have to pass it explicitly.
		if actor, ok := authctx.ActorFromContext(ctx); ok && actor.NurseryID > 0 {
			ctx = auditlog.WithNurseryID(ctx, actor.NurseryID)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
