package tracking

import (
	"database/sql"
	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	r := NewRepository(db)
	s := NewService(r)
	return Module{handler: NewHandler(s, jwt)}
}
func (m Module) RegisterRoutes(r chi.Router) {
	r.Post("/tracking", m.handler.Create)
	r.Get("/dispatches/{dispatchId}/tracking", m.handler.ListDispatch)
	r.Get("/dispatches/{dispatchId}/tracking/latest", m.handler.LatestDispatch)
	r.Get("/drivers/{driverId}/tracking", m.handler.ListDriver)
	r.Get("/drivers/{driverId}/tracking/latest", m.handler.LatestDriver)
	r.Get("/vehicles/{vehicleId}/tracking", m.handler.ListVehicle)
	r.Get("/vehicles/{vehicleId}/tracking/latest", m.handler.LatestVehicle)
}
