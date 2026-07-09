package subscriptions

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	handler *Handler
	service *Service
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	repository := NewRepository(db)
	service := NewService(repository)
	return Module{handler: NewHandler(service, jwt), service: service}
}

// Service exposes the subscription service so other modules can call CreateTrialForOwner.
func (m Module) Service() *Service { return m.service }

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/subscription-plans", func(r chi.Router) {
		r.Get("/", m.handler.ListPlans)
		r.Get("/{id}", m.handler.GetPlan)
		r.Put("/{id}", m.handler.UpdatePlan)
	})

	router.Route("/subscription-promos", func(r chi.Router) {
		r.Get("/", m.handler.ListPromos)
		r.Post("/", m.handler.CreatePromo)
		r.Put("/{id}", m.handler.UpdatePromo)
		r.Post("/validate", m.handler.ValidatePromo)
		r.Post("/{id}/blast", m.handler.BlastPromo)
	})

	router.Route("/subscriptions", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/me", m.handler.Me)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}/status", m.handler.UpdateStatus)
		r.Post("/{id}/renew", m.handler.Renew)
		r.Post("/{id}/cancel", m.handler.Cancel)
		r.Get("/{id}/payments", m.handler.ListPayments)
	})
}
