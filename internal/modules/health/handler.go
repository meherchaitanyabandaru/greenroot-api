package health

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

type Handler struct {
	version string
	env     string
}

func NewHandler(version string, env string) Handler {
	return Handler{version: version, env: env}
}

func (h Handler) Register(router chi.Router) {
	router.Get("/health", h.Health)
	router.Get("/healthz", h.Health)
	router.Get("/readyz", h.Ready)
}

func (h Handler) Health(w http.ResponseWriter, r *http.Request) {
	response.OK(w, response.Envelope{
		"status":    "ok",
		"service":   "greenroot-api",
		"version":   h.version,
		"env":       h.env,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h Handler) Ready(w http.ResponseWriter, r *http.Request) {
	response.OK(w, response.Envelope{
		"status": "ready",
		"checks": response.Envelope{
			"http":     "ok",
			"postgres": "configured",
		},
	})
}
