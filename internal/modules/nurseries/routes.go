package nurseries

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, redisClients ...*redis.Client) Module {
	return NewModuleWithTrial(db, jwt, audit, nil, redisClients...)
}

func NewModuleWithTrial(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, trialSvc TrialCreator, redisClients ...*redis.Client) Module {
	var rdb *redis.Client
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	repository := NewRepository(db)
	var service *Service
	if rdb != nil {
		service = NewServiceWithTrial(repository, trialSvc, audit, rdb)
	} else {
		service = NewServiceWithTrial(repository, trialSvc, audit)
	}
	return Module{handler: NewHandler(service, jwt)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/nurseries", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/mine", m.handler.Mine)                   // nurseries where user is manager
		r.Get("/owned", m.handler.OwnedNursery)          // nursery user owns
		r.Post("/owned/resubmit", m.handler.Resubmit)    // resubmit a rejected application
		r.Get("/{id}", m.handler.Get)
		r.Put("/{id}", m.handler.Update)
		r.Put("/{id}/status", m.handler.UpdateStatus)
		r.Delete("/{id}", m.handler.Delete)

		// Addresses
		r.Get("/{id}/addresses", m.handler.ListAddresses)
		r.Post("/{id}/addresses", m.handler.CreateAddress)
		r.Put("/addresses/{addressId}", m.handler.UpdateAddress)
		r.Delete("/addresses/{addressId}", m.handler.DeleteAddress)

		// Managers (V1: owner-managed, text role)
		r.Get("/{id}/users", m.handler.ListUsers)
		r.Get("/{id}/managers", m.handler.ListManagers)
		r.Post("/{id}/managers", m.handler.AddManager)
		r.Delete("/{id}/managers/{userId}", m.handler.RemoveUser)

		// Self-leave (manager or driver initiated)
		r.Delete("/me/leave", m.handler.LeaveNursery)

		// Driver connections
		r.Get("/{id}/drivers", m.handler.ListDrivers)
		r.Post("/{id}/drivers", m.handler.ConnectDriver)
		r.Post("/{id}/drivers/{driverUserId}/approve", m.handler.ApproveDriver)
		r.Delete("/{id}/drivers/{driverUserId}", m.handler.DisconnectDriver)

		// Customers
		r.Get("/{id}/customers", m.handler.GetCustomers)
	})
}
