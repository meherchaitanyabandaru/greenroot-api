package nurseries

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
	router.Route("/nurseries", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Delete("/{id}", m.handler.Delete)
		r.Get("/{id}/addresses", m.handler.ListAddresses)
		r.Post("/{id}/addresses", m.handler.CreateAddress)
		r.Put("/addresses/{addressId}", m.handler.UpdateAddress)
		r.Delete("/addresses/{addressId}", m.handler.DeleteAddress)
		r.Get("/{id}/users", m.handler.ListUsers)
		r.Post("/{id}/users", m.handler.AddUser)
		r.Delete("/{id}/users/{userId}", m.handler.RemoveUser)
	})
}
