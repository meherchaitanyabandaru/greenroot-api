package attachments

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	repo := NewRepository(db)
	svc := NewService(repo)
	return Module{handler: NewHandler(svc, jwt)}
}
func (m Module) RegisterRoutes(r chi.Router) {
	r.Route("/attachments", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Delete("/{id}", m.handler.Delete)
	})
}
