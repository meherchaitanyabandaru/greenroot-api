package plants

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
	router.Route("/plants", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/sizes", m.handler.Sizes)
		r.Get("/categories", m.handler.Categories)
		r.Post("/categories", m.handler.CreateCategory)
		r.Put("/categories/{categoryId}", m.handler.UpdateCategory)
		r.Delete("/categories/{categoryId}", m.handler.DeleteCategory)
		r.Get("/names", m.handler.Names)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Delete("/{id}", m.handler.Delete)
		r.Post("/{id}/images", m.handler.CreateImage)
		r.Get("/{id}/care-guide", m.handler.CareGuide)
	})
}
