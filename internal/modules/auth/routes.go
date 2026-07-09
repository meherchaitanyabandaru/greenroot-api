package auth

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
	service := NewService(repository, jwt, audit)
	return Module{handler: NewHandler(service)}
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/auth", func(r chi.Router) {
		r.Post("/send-otp", m.handler.SendOTP)
		r.Post("/verify-otp", m.handler.VerifyOTP)
		r.Post("/refresh-token", m.handler.RefreshToken)
		r.Post("/logout", m.handler.Logout)
	})

	// Workspace endpoint lives under /me for cleaner mobile routing
	router.Get("/me/workspaces", m.handler.Workspaces)
	router.Get("/me/owner-dashboard", m.handler.OwnerDashboard)
}
