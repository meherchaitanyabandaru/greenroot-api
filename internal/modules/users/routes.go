package users

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
	router.Route("/users", func(r chi.Router) {
		r.Get("/me", m.handler.Me)
		r.Put("/me", m.handler.UpdateMe)
		r.Get("/{id}", m.handler.GetUser)
		r.Get("/{id}/addresses", m.handler.ListAddresses)
		r.Post("/{id}/addresses", m.handler.CreateAddress)
		r.Put("/addresses/{addressId}", m.handler.UpdateAddress)
		r.Delete("/addresses/{addressId}", m.handler.DeleteAddress)
		r.Get("/{id}/roles", m.handler.ListRoles)
		r.Get("/{id}/sessions", m.handler.ListSessions)
	})
}
