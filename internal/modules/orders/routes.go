package orders

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
	router.Route("/orders", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/{id}", m.handler.Get)
		r.Get("/{id}/documents/render", m.handler.RenderDocument)
		r.Put("/{id}/status", m.handler.UpdateStatus)
		r.Put("/{id}/delivery", m.handler.UpdateDeliverySnapshot)
		r.Delete("/{id}", m.handler.Delete)
		// V1 order lifecycle
		r.Post("/{id}/confirm", m.handler.ConfirmOrder)
		// V1 loading workflow
		r.Post("/{id}/start-loading", m.handler.StartLoading)
		r.Post("/{id}/complete-loading", m.handler.CompleteLoading)
		r.Post("/{id}/cancel", m.handler.CancelOrder)
		r.Post("/{id}/assign-manager", m.handler.AssignManager)
		// Items
		r.Get("/{id}/items", m.handler.ListItems)
		r.Post("/{id}/items", m.handler.CreateItem)
		r.Put("/{id}/items/{itemId}", m.handler.UpdateItem)
		r.Delete("/{id}/items/{itemId}", m.handler.DeleteItem)
		r.Put("/{id}/items/{itemId}/loaded-quantity", m.handler.SetLoadedQuantity)
		// Verification token endpoints (auth required)
		r.Post("/{id}/verify-token", m.handler.GetOrCreateVerifyToken)
		r.Post("/{id}/verify-token/revoke", m.handler.RevokeAndRegenerateToken)
	})
	// Public verification — no auth, rate-limited. Distinct path from quotations'
	// /verify/{token} (registered separately by the quotations module on the same
	// router) so the two don't collide; the mobile QR classifier recognizes both.
	router.Get("/verify-order/{token}", m.handler.PublicVerify)
}
