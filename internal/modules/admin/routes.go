package admin

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service, redisClients ...*redis.Client) Module {
	var rdb *redis.Client
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	r := NewRepository(db)
	s := NewService(r, rdb)
	return Module{handler: NewHandler(s, jwt)}
}
func (m Module) RegisterRoutes(r chi.Router) {
	r.Route("/admin", func(r chi.Router) {
		r.Get("/dashboard", m.handler.Dashboard)
		r.Get("/users", m.handler.ListUsers)
		r.Put("/users/{id}/status", m.handler.UpdateUserStatus)
		r.Put("/nurseries/{id}/status", m.handler.UpdateNurseryStatus)
	})
}
