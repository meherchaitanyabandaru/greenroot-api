package quotations

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	repository := NewRepository(db)
	service := NewService(repository)
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/quotations", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Delete("/{id}", m.handler.Delete)
		r.Post("/{id}/assign-manager", m.handler.AssignManager)
		r.Post("/{id}/approve", m.handler.Approve)
		r.Post("/{id}/convert-to-order", m.handler.ConvertToOrder)
		// Buyer actions
		r.Post("/{id}/buyer-accept", m.handler.BuyerAccept)
		r.Post("/{id}/buyer-reject", m.handler.BuyerReject)
	})
}
