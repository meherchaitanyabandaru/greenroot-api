package storage

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

type Module struct{ handler *Handler }

func NewModule(_ *sql.DB, jwt *jwtplatform.Service, s *platformstorage.Client) Module {
	return Module{handler: NewHandler(s, jwt)}
}

func (m Module) RegisterRoutes(r chi.Router) {
	r.Route("/storage", func(r chi.Router) {
		r.Post("/presign", m.handler.Presign)
	})
}
