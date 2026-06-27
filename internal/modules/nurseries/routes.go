package nurseries

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
	router.Route("/nurseries", func(r chi.Router) {
		r.Get("/", m.handler.List)
		r.Post("/", m.handler.Create)
		r.Get("/mine", m.handler.Mine)           // nurseries where user is manager
		r.Get("/owned", m.handler.OwnedNursery)  // nursery user owns
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
		r.Get("/{id}/managers", m.handler.ListManagers)
		r.Post("/{id}/managers", m.handler.AddManager)
		r.Delete("/{id}/managers/{userId}", m.handler.RemoveUser)

		// Driver connections
		r.Get("/{id}/drivers", m.handler.ListDrivers)
		r.Post("/{id}/drivers", m.handler.ConnectDriver)
		r.Post("/{id}/drivers/{driverUserId}/approve", m.handler.ApproveDriver)
	})
}
