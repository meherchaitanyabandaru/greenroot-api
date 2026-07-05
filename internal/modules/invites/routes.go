package invites

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
	router.Route("/invites", func(r chi.Router) {
		r.Post("/", m.handler.Create)
		r.Get("/{uuid}", m.handler.GetByUUID)
		r.Post("/{uuid}/accept", m.handler.Accept)
		r.Post("/{uuid}/cancel", m.handler.Cancel)
	})
	router.Get("/nurseries/{nurseryId}/invites", m.handler.ListByNursery)
	router.Get("/me/connections", m.handler.GetMyConnections)
}
