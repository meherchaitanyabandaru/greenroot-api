package users

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/imagepolicy"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

type Module struct {
	handler *Handler
}

func NewModule(db *sql.DB, jwt *jwtplatform.Service, storage *platformstorage.Client, audit *auditlog.Service, rdb *redis.Client, emailSender EmailSender, smsSender SMSSender) Module {
	repository := NewRepository(db)
	var service *Service
	if rdb != nil {
		service = NewService(repository, storage, audit, rdb)
	} else {
		service = NewService(repository, storage, audit)
	}
	if emailSender != nil {
		service.WithEmail(emailSender)
	}
	if smsSender != nil {
		service.WithSMS(smsSender)
	}
	return Module{handler: NewHandler(service, jwt)}
}

// WithImagePolicies checks profile photos against the SELFIE image policy
// (without it, the built-in default policy).
func (m Module) WithImagePolicies(policies *imagepolicy.Store) Module {
	m.handler.policies = policies
	return m
}

func (m Module) RegisterRoutes(router chi.Router) {
	router.Route("/users", func(r chi.Router) {
		r.Get("/me", m.handler.Me)
		r.Put("/me", m.handler.UpdateMe)
		r.Post("/me/onboarding", m.handler.CompleteOnboarding)
		r.Delete("/me", m.handler.DeleteAccount)
		r.Post("/me/avatar", m.handler.UploadAvatar)
		r.Delete("/me/avatar", m.handler.DeleteAvatar)
		r.Post("/me/email/send-code", m.handler.SendEmailCode)
		r.Post("/me/email/verify", m.handler.VerifyEmail)
		r.Post("/me/mobile/send-code", m.handler.SendMobileCode)
		r.Post("/me/mobile/verify", m.handler.VerifyMobile)
		r.Get("/{id}", m.handler.GetUser)
		r.Get("/{id}/addresses", m.handler.ListAddresses)
		r.Post("/{id}/addresses", m.handler.CreateAddress)
		r.Put("/addresses/{addressId}", m.handler.UpdateAddress)
		r.Delete("/addresses/{addressId}", m.handler.DeleteAddress)
		r.Get("/{id}/roles", m.handler.ListRoles)
		r.Get("/{id}/sessions", m.handler.ListSessions)
	})
}
