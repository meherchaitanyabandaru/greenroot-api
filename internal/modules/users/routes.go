package users

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, storage *platformstorage.Client, audit *auditlog.Service) Module {
	repository := NewRepository(db)
	service := NewService(repository, storage, audit)
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/users", func(r chi.Router) {
		r.Get("/me", m.handler.Me)
		r.Put("/me", m.handler.UpdateMe)
		r.Delete("/me", m.handler.DeleteAccount)
		r.Post("/me/avatar", m.handler.UploadAvatar)
		r.Get("/{id}", m.handler.GetUser)
		r.Get("/{id}/addresses", m.handler.ListAddresses)
		r.Post("/{id}/addresses", m.handler.CreateAddress)
		r.Put("/addresses/{addressId}", m.handler.UpdateAddress)
		r.Delete("/addresses/{addressId}", m.handler.DeleteAddress)
		r.Get("/{id}/roles", m.handler.ListRoles)
		r.Get("/{id}/sessions", m.handler.ListSessions)
	})
}
