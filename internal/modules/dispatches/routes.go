package dispatches

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
	// Public tracking (no auth required — registered before protected routes)
	router.Get("/track/{uuid}", m.handler.PublicTracking)

	router.Route("/dispatches", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/code/{code}", m.handler.GetByCode) // look up by dispatch_code (for driver join flow)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}/status", m.handler.UpdateStatus)
		r.Post("/{id}/accept", m.handler.Accept) // driver accepts and links to dispatch
		r.Post("/{id}/items", m.handler.CreateItem)
		r.Post("/{id}/trip-events", m.handler.CreateTripEvent)
	})
	router.Get("/orders/{orderId}/dispatches", m.handler.ListByOrder)
}
