package sms

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformsms "github.com/meherchaitanyabandaru/greenroot-api/platform/sms"
)

type Module struct {
	service *Service
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, provider platformsms.Provider) Module {
	service := NewService(NewRepository(db), provider, audit)
	return Module{service: service, handler: NewHandler(service, jwt)}
}

// Service is shared with auth (OTP) and payments (alerts).
func (m Module) Service() *Service { return m.service }

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/sms", func(r chi.Router) {
		r.Get("/status", m.handler.Status)
		r.Get("/entity", m.handler.GetEntity)
		r.Put("/entity", m.handler.UpdateEntity)
		r.Get("/headers", m.handler.ListHeaders)
		r.Post("/headers", m.handler.CreateHeader)
		r.Put("/headers/{id}", m.handler.UpdateHeader)
		r.Get("/templates", m.handler.ListTemplates)
		r.Post("/templates", m.handler.CreateTemplate)
		r.Post("/templates/preview", m.handler.Preview)
		r.Get("/templates/{id}", m.handler.GetTemplate)
		r.Put("/templates/{id}", m.handler.UpdateTemplate)
		r.Post("/templates/{id}/test", m.handler.TestSend)
		r.Get("/messages", m.handler.ListMessages)
		r.Get("/preferences/me", m.handler.GetMyPreferences)
		r.Put("/preferences/me", m.handler.UpdateMyPreferences)
	})
}
