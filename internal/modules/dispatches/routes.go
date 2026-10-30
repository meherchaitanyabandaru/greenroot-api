package dispatches

import (
	"database/sql"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/consent"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	handler *Handler
	service *Service
}

// codeKey is the server secret delivery codes are derived from.
func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, codeKey []byte, redisClients ...*redis.Client) Module {
	repository := NewRepository(db)
	var rdb *redis.Client
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	var service *Service
	if rdb != nil {
		service = NewService(repository, audit, rdb)
	} else {
		service = NewService(repository, audit)
	}
	service.SetDeliveryCodeKey(codeKey)
	return Module{handler: NewHandler(service, jwt), service: service}
}

// WithConsents stores the parties' confirmations.
func (m Module) WithConsents(c *consent.Store) Module {
	m.service.UseConsents(c)
	return m
}

// WithEvents sends delivery events by SMS/email too (messaging.Notifier).
func (m Module) WithEvents(n EventNotifier) Module {
	m.service.SetEvents(n)
	return m
}

func (m Module) RegisterRoutes(router chi.Router) {
	// Public tracking (no auth required — registered before protected routes)
	router.Get("/track/{uuid}", m.handler.PublicTracking)

	router.Route("/dispatches", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/code/{code}", m.handler.GetByCode) // driver join preview: the trip code from the QR, never the dispatch code
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}/status", m.handler.UpdateStatus)
		r.Post("/{id}/ack-delivery-update", m.handler.AcknowledgeDeliveryUpdate)
		r.Post("/{id}/deliver", m.handler.Deliver)      // driver/nursery: customer's code or override
		r.Post("/{id}/receipt", m.handler.Receipt)      // customer: received / report a problem
		r.Post("/{id}/accept", m.handler.Accept)        // driver accepts and links to dispatch
		r.Post("/{id}/new-invite", m.handler.NewInvite) // staff: new trip QR (old one stops working)
		r.Post("/{id}/items", m.handler.CreateItem)
		r.Post("/{id}/trip-events", m.handler.CreateTripEvent)
	})
	router.Get("/orders/{orderId}/dispatches", m.handler.ListByOrder)
}
