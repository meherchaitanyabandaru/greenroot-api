package orders

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	repository := NewRepository(db)
	service := NewService(repository)
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/orders", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}/status", m.handler.UpdateStatus)
		r.Delete("/{id}", m.handler.Delete)
		r.Get("/{id}/items", m.handler.ListItems)
		r.Post("/{id}/items", m.handler.CreateItem)
		r.Put("/items/{itemId}", m.handler.UpdateItem)
		r.Delete("/items/{itemId}", m.handler.DeleteItem)
	})
}
