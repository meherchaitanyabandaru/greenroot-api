package search

import (
	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	handler *Handler
}

func NewModule(jwt *jwtplatform.Service, redisClients ...*redis.Client) Module {
	var rdb *redis.Client
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	var service *Service
	if rdb != nil {
		service = NewService(rdb)
	} else {
		service = NewService()
	}
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(r chi.Router) {
	r.Route("/search", func(r chi.Router) {
		r.Get("/recent", m.handler.GetRecent)
		r.Post("/recent", m.handler.RecordRecent)
		r.Delete("/recent", m.handler.ClearRecent)
		r.Get("/suggestions", m.handler.Suggestions)
	})
}
