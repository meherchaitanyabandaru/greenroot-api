package payments

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
	router.Route("/payments", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/manual", m.handler.CreateManual)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}/status", m.handler.UpdateStatus)
	})
	router.Get("/orders/{orderId}/payments", m.handler.ListByOrder)
	router.Get("/subscriptions/{subscriptionId}/payments", m.handler.ListBySubscription)
}
