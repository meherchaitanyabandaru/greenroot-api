package sourcing

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service) Module {
	repo := NewRepository(db)
	svc := NewService(repo, audit)
	return Module{handler: NewHandler(svc, jwt)}
}

func (m Module) RegisterRoutes(r chi.Router) {
	// Nursery membership (join / leave / status)
	r.Route("/nurseries/{nurseryId}/sourcing-membership", func(r chi.Router) {
		r.Get("/", m.handler.GetMembership)
		r.Post("/", m.handler.JoinNetwork)
		r.Delete("/", m.handler.LeaveNetwork)
	})

	// Network discovery — returns only approved, publicly safe nursery info
	r.Route("/sourcing-network/nurseries", func(r chi.Router) {
		r.Get("/", m.handler.ListNearby)
		r.Get("/{nurseryId}", m.handler.GetNurseryProfile)
	})
}
