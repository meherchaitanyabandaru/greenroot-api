package invites

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
	var rdb *redis.Client
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	repository := NewRepository(db)
	var service *Service
	if rdb != nil {
		service = NewService(repository, audit, rdb)
	} else {
		service = NewService(repository, audit)
	}
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/invites", func(r chi.Router) {
		r.Post("/", m.handler.Create)
		r.Get("/{uuid}", m.handler.GetByUUID)
		r.Post("/{uuid}/accept", m.handler.Accept)
		r.Post("/{uuid}/cancel", m.handler.Cancel)
	})
	router.Get("/nurseries/{nurseryId}/invites", m.handler.ListByNursery)
	router.Get("/me/connections", m.handler.GetMyConnections)
}
