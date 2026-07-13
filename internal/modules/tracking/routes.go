package tracking

import (
	"database/sql"
	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisgeo"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service, redisClients ...*redis.Client) Module {
	r := NewRepository(db)
	var liveGeo *redisgeo.Service
	if len(redisClients) > 0 && redisClients[0] != nil {
		liveGeo = redisgeo.New(redisClients[0])
	}
	s := NewService(r, liveGeo)
	return Module{handler: NewHandler(s, jwt)}
}
func (m Module) RegisterRoutes(r chi.Router) {
	r.Post("/tracking", m.handler.Create)
	r.Post("/tracking/live", m.handler.UpdateLiveLocation)
	r.Get("/tracking/live/drivers/{driverUserId}", m.handler.GetLiveDriver)
	r.Get("/tracking/live/nearby", m.handler.NearbyLiveDrivers)
	r.Get("/dispatches/{dispatchId}/tracking", m.handler.ListDispatch)
	r.Get("/dispatches/{dispatchId}/tracking/latest", m.handler.LatestDispatch)
	r.Get("/drivers/{driverId}/tracking", m.handler.ListDriver)
	r.Get("/drivers/{driverId}/tracking/latest", m.handler.LatestDriver)
	r.Get("/vehicles/{vehicleId}/tracking", m.handler.ListVehicle)
	r.Get("/vehicles/{vehicleId}/tracking/latest", m.handler.LatestVehicle)
}
