package sourcing

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	repo := NewRepository(db)
	svc := NewService(repo)
	return Module{handler: NewHandler(svc, jwt)}
}

func (m Module) RegisterRoutes(r chi.Router) {
	// Nursery membership (join / leave / status)
	r.Route("/nurseries/{nurseryId}/sourcing-membership", func(r chi.Router) {
		r.Get("/", m.handler.GetMembership)
		r.Post("/", m.handler.JoinNetwork)
		r.Delete("/", m.handler.LeaveNetwork)
	})

	// Featured plants (top-20 "we usually have this" per nursery)
	r.Route("/nurseries/{nurseryId}/featured-plants", func(r chi.Router) {
		r.Get("/", m.handler.ListFeaturedPlants)
		r.Post("/", m.handler.AddFeaturedPlant)
		r.Put("/{featuredId}", m.handler.UpdateFeaturedPlant)
		r.Delete("/{featuredId}", m.handler.DeleteFeaturedPlant)
	})

	// Network discovery — returns only approved, publicly safe nursery info
	r.Route("/sourcing-network/nurseries", func(r chi.Router) {
		r.Get("/", m.handler.ListNearby)
		r.Get("/{nurseryId}", m.handler.GetNurseryProfile)
	})

	// Sourcing posts (NEED / AVAILABLE announcements)
	r.Route("/sourcing-posts", func(r chi.Router) {
		r.Get("/", m.handler.ListPosts)
		r.Post("/", m.handler.CreatePost)
		r.Get("/{id}", m.handler.GetPost)
		r.Put("/{id}", m.handler.UpdatePost)
		r.Delete("/{id}", m.handler.DeletePost)
		r.Get("/{id}/responses", m.handler.ListResponses)
		r.Post("/{id}/responses", m.handler.CreateResponse)
		r.Put("/{id}/responses/{responseId}", m.handler.UpdateResponse)
	})
}
