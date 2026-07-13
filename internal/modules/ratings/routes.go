package ratings

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	repo := NewRepository(db)
	svc := NewService(repo)
	return Module{handler: NewHandler(svc, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/ratings", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/app", m.handler.SubmitApp)
		r.Post("/order/{order_id}", m.handler.SubmitOrder)
		r.Get("/order/{order_id}", m.handler.GetMyOrderRating)
		r.Post("/trip/{dispatch_id}", m.handler.SubmitTrip)
		r.Get("/trip/{dispatch_id}", m.handler.GetMyTripRating)
	})
}
