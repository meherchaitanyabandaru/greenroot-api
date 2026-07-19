package auth

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	appmiddleware "github.com/meherchaitanyabandaru/greenroot-api/internal/middleware"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	handler *Handler
	rdb     *redis.Client
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, rdb *redis.Client) Module {
	repository := NewRepository(db)
	var service *Service
	if rdb != nil {
		service = NewService(repository, jwt, audit, rdb)
	} else {
		service = NewService(repository, jwt, audit)
	}
	return Module{handler: NewHandler(service), rdb: rdb}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/auth", func(r chi.Router) {
		r.With(appmiddleware.OTPRateLimit(m.rdb)).Post("/send-otp", m.handler.SendOTP)
		r.With(appmiddleware.VerifyRateLimit(m.rdb)).Post("/verify-otp", m.handler.VerifyOTP)
		r.Post("/refresh-token", m.handler.RefreshToken)
		r.Post("/logout", m.handler.Logout)
		r.Get("/me", m.handler.Me)
	})

	// Workspace endpoint lives under /me for cleaner mobile routing
	router.Get("/me/workspaces", m.handler.Workspaces)
	router.Get("/me/owner-dashboard", m.handler.OwnerDashboard)
}
