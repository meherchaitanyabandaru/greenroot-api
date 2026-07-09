package notifications

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service) Module {
	repository := NewRepository(db)
	service := NewService(repository, MockSender{}, audit)
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/notifications", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Put("/read-all", m.handler.MarkAllRead)
		r.Get("/devices", m.handler.ListDevices)
		r.Post("/devices", m.handler.UpsertDevice)
		r.Delete("/devices/{id}", m.handler.DeleteDevice)
		r.Get("/templates", m.handler.ListTemplates)
		r.Post("/templates", m.handler.CreateTemplate)
		r.Put("/templates/{id}", m.handler.UpdateTemplate)
		r.Delete("/templates/{id}", m.handler.DeleteTemplate)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}/read", m.handler.MarkRead)
		r.Delete("/{id}", m.handler.Delete)
	})
}
