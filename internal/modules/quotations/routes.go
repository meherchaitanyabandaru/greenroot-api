package quotations

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

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, storageCli *storage.Client, redisClients ...*redis.Client) Module {
	repository := NewRepository(db)
	var rdb *redis.Client
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	var service *Service
	if rdb != nil {
		service = NewService(repository, audit, storageCli, rdb)
	} else {
		service = NewService(repository, audit, storageCli)
	}
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/quotations", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		// by-token must be registered before /{id} to avoid chi matching "by-token" as an id
		r.Get("/by-token/{token}", m.handler.GetByToken)
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
		// Document (PDF storage) endpoints
		r.Post("/{id}/documents", m.handler.UploadDocument)
		r.Get("/{id}/documents/current", m.handler.GetCurrentDocument)
		r.Get("/{id}/documents/render", m.handler.RenderDocument)
		r.Get("/{id}/documents", m.handler.ListDocuments)
		// Verification token endpoints (auth required)
		r.Post("/{id}/verify-token", m.handler.GetOrCreateVerifyToken)
		r.Post("/{id}/verify-token/revoke", m.handler.RevokeAndRegenerateToken)
	})
	// Public verification — no auth, rate-limited
	router.Get("/verify/{token}", m.handler.PublicVerify)
}
