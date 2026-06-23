package audit

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
func (m Module) RegisterRoutes(r chi.Router) { r.Get("/audit-logs", m.handler.List) }
