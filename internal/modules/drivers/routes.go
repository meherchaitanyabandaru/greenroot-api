package drivers

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, files *storage.Client, redisClients ...*redis.Client) Module {
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
	if files != nil { // a nil *Client in the interface would not read as nil
		service.UseStorage(files)
	}
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/drivers", func(r chi.Router) {
		// V1 self-service endpoints (must be before /{id} to avoid route conflicts)
		r.Post("/apply", m.handler.Apply)
		r.Get("/me", m.handler.GetMine)
		r.Delete("/me", m.handler.StopDriving)

		// Admin endpoints
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Delete("/{id}", m.handler.Delete)
		r.Post("/{id}/approve", m.handler.ApproveDriver)
		r.Post("/{id}/location", m.handler.CreateLocation)
	})
}
