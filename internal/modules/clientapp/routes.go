package clientapp

import (
	"database/sql"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	appmiddleware "github.com/meherchaitanyabandaru/greenroot-api/internal/middleware"
	"github.com/redis/go-redis/v9"
)

// Module wires the clientapp routes.
type Module struct {
	handler *Handler
	rdb     *redis.Client
}

// NewModule builds the module. rdb may be nil (no rate limit without Redis).
func NewModule(db *sql.DB, policy config.ClientAppConfig, rdb *redis.Client) Module {
	return Module{handler: NewHandler(NewService(NewRepository(db), policy)), rdb: rdb}
}

// RegisterRoutes mounts the routes under /api/v1.
func (m Module) RegisterRoutes(r chi.Router) {
	r.Get("/app/version-policy", m.handler.VersionPolicy)
	// Public so a crash before sign-in is still reported; 30 per IP per 10 min.
	r.With(appmiddleware.RateLimit(m.rdb, "crash", 30, 10*time.Minute)).
		Post("/app/crash-reports", m.handler.SubmitCrashReport)
	r.Get("/admin/crash-reports", m.handler.ListCrashReports)
}
