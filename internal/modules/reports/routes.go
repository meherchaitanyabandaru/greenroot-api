package reports

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	return Module{handler: NewHandler(NewService(NewRepository(db)), jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Get("/reports/{type}", m.handler.Get)
}
