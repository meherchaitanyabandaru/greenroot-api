package drivers

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service) Module {
	repository := NewRepository(db)
	service := NewService(repository, audit)
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/drivers", func(r chi.Router) {
		// V1 self-service endpoints (must be before /{id} to avoid route conflicts)
		r.Post("/apply", m.handler.Apply)
		r.Get("/me", m.handler.GetMine)

		// Admin endpoints
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Delete("/{id}", m.handler.Delete)
		r.Post("/{id}/approve", m.handler.ApproveDriver)
		r.Post("/{id}/location", m.handler.CreateLocation)
	})
}
