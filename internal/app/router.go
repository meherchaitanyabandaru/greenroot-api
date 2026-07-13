package app

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	appmiddleware "github.com/meherchaitanyabandaru/greenroot-api/internal/middleware"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/admin"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/attachments"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/audit"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/auth"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/dispatches"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/drivers"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/health"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/inventory"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/invites"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/market"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/notifications"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/nurseries"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/orders"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/payments"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/plantrequests"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/plants"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/quotations"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/ratings"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sourcing"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/storage"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/subscriptions"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/tracking"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/users"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/vehicles"
)

func NewRouter(deps Dependencies) chi.Router {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(appmiddleware.CORS(deps.Config.HTTP.CORSAllowedOrigins))
	router.Use(appmiddleware.Recovery(deps.Logger))
	router.Use(appmiddleware.SecurityHeaders)
	router.Use(appmiddleware.RequestLogger(deps.Logger))
	// AuditContext must come after RequestID and after EnrichActorMiddleware
	// (registered below on the /api/v1 sub-router) so we apply it there too.

	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		response.Error(w, http.StatusNotFound, "route_not_found", "route not found")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		response.Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	health.NewHandler(deps.Config.App.Version, deps.Config.App.Env).Register(router)
	registerDocsRoutes(router)

	router.Route("/api/v1", func(r chi.Router) {
		r.Use(authctx.EnrichActorMiddleware(deps.JWT, deps.Redis))
		r.Use(appmiddleware.AuditContext) // must come after EnrichActorMiddleware

		auth.NewModule(deps.DB, deps.JWT, deps.Audit, deps.Redis).RegisterRoutes(r)
		admin.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		attachments.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		audit.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		dispatches.NewModule(deps.DB, deps.JWT, deps.Audit, deps.Redis).RegisterRoutes(r)
		drivers.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		inventory.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		market.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		invites.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		notifications.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		subModule := subscriptions.NewModule(deps.DB, deps.JWT, deps.Audit, deps.Redis)
		nurseries.NewModuleWithTrial(deps.DB, deps.JWT, deps.Audit, subModule.Service()).RegisterRoutes(r)
		orders.NewModule(deps.DB, deps.JWT, deps.Audit, deps.Redis).RegisterRoutes(r)
		quotations.NewModule(deps.DB, deps.JWT, deps.Audit, deps.Storage, deps.Redis).RegisterRoutes(r)
		payments.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		plants.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		plantrequests.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		ratings.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		sourcing.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		storage.NewModule(deps.DB, deps.JWT, deps.Storage).RegisterRoutes(r)
		subModule.RegisterRoutes(r)
		tracking.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		vehicles.NewModule(deps.DB, deps.JWT, deps.Audit).RegisterRoutes(r)
		users.NewModule(deps.DB, deps.JWT, deps.Storage, deps.Audit, deps.Redis).RegisterRoutes(r)
	})

	return router
}

func registerDocsRoutes(router chi.Router) {
	router.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("docs", "swagger", "openapi.yaml"))
	})
	router.Get("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/index.html", http.StatusMovedPermanently)
	})
	router.Get("/swagger/", swaggerHTML)
	router.Get("/swagger/index.html", swaggerHTML)
}

func swaggerHTML(w http.ResponseWriter, r *http.Request) {
	html, err := os.ReadFile(filepath.Join("docs", "swagger", "index.html"))
	if err != nil {
		http.Error(w, "swagger ui not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}
