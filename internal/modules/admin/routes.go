package admin

import (
	"database/sql"
	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	r := NewRepository(db)
	s := NewService(r)
	return Module{handler: NewHandler(s, jwt)}
}
func (m Module) RegisterRoutes(r chi.Router) {
	r.Route("/admin", func(r chi.Router) {
		r.Get("/dashboard", m.handler.Dashboard)
		r.Get("/users", m.handler.ListUsers)
	})
}
