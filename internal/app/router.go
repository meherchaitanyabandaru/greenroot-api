package app

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/notifications"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/nurseries"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/orders"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/quotations"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/payments"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/plants"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/requests"
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
	router.Use(appmiddleware.Audit(deps.Logger))

	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		response.Error(w, http.StatusNotFound, "route_not_found", "route not found")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		response.Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	health.NewHandler(deps.Config.App.Version, deps.Config.App.Env).Register(router)
	registerDocsRoutes(router)

	router.Route("/api/v1", func(r chi.Router) {
		auth.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		admin.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		attachments.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		audit.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		dispatches.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		drivers.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		inventory.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		invites.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		notifications.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		nurseries.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		orders.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		quotations.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		payments.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		plants.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		requests.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		sourcing.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		storage.NewModule(deps.DB, deps.JWT, deps.Storage).RegisterRoutes(r)
		subscriptions.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		tracking.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		vehicles.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
		users.NewModule(deps.DB, deps.JWT).RegisterRoutes(r)
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
