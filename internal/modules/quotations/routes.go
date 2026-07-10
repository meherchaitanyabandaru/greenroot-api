package quotations

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service) Module {
	repository := NewRepository(db)
	service := NewService(repository, audit)
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/quotations", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Put("/{id}/customer", m.handler.UpdateCustomer)
		r.Delete("/{id}", m.handler.Delete)
		r.Post("/{id}/assign-manager", m.handler.AssignManager)
		r.Delete("/{id}/assign-manager", m.handler.UnassignManager)
		r.Post("/{id}/send", m.handler.SendToCustomer)
		r.Post("/{id}/approve", m.handler.Approve)
		r.Post("/{id}/recall", m.handler.Recall)
		r.Post("/{id}/convert-to-order", m.handler.ConvertToOrder)
		r.Post("/{id}/record-download", m.handler.RecordDownload)
		// Buyer actions
		r.Post("/{id}/buyer-accept", m.handler.BuyerAccept)
		r.Post("/{id}/buyer-reject", m.handler.BuyerReject)
	})
}
