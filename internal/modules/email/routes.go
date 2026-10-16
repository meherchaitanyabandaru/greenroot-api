package email

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	platformemail "github.com/meherchaitanyabandaru/greenroot-api/platform/email"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	service *Service
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, provider platformemail.Provider) Module {
	service := NewService(NewRepository(db), provider, audit)
	return Module{service: service, handler: NewHandler(service, jwt)}
}

// Service is shared with users (verification codes) and the messaging notifier.
func (m Module) Service() *Service { return m.service }

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/email", func(r chi.Router) {
		r.Get("/status", m.handler.Status)
		r.Get("/sender", m.handler.GetSender)
		r.Put("/sender", m.handler.UpdateSender)
		r.Get("/templates", m.handler.ListTemplates)
		r.Post("/templates", m.handler.CreateTemplate)
		r.Post("/templates/preview", m.handler.Preview)
		r.Get("/templates/{id}", m.handler.GetTemplate)
		r.Put("/templates/{id}", m.handler.UpdateTemplate)
		r.Post("/templates/{id}/test", m.handler.TestSend)
		r.Get("/messages", m.handler.ListMessages)
	})
}
