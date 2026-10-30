package vehicles

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, redisClients ...*redis.Client) Module {
	repository := NewRepository(db)
	service := NewService(repository, audit)
	if len(redisClients) > 0 && redisClients[0] != nil {
		service.UseNotifications(redisClients[0], db)
	} else {
		service.UseNotifications(nil, db)
	}
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/vehicles", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Delete("/{id}", m.handler.Delete)
		r.Put("/{id}/approval", m.handler.SetApproval)
	})
	// Admin: lorry owners (no GreenRoot account needed) and driver ↔ lorry links.
	router.Route("/vehicle-owners", func(r chi.Router) {
		r.Get("/", m.handler.ListOwners)
		r.Post("/", m.handler.SaveOwner)
		r.Get("/{id}", m.handler.GetOwner)
		r.Put("/{id}", m.handler.SaveOwner)
	})
	router.Route("/vehicle-drivers", func(r chi.Router) {
		r.Get("/", m.handler.ListLinks)
		r.Put("/{id}/status", m.handler.SetLinkStatus)
	})
}
