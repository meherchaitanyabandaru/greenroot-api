package storage

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/imagepolicy"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

type Module struct{ handler *Handler }

// policies is shared with the modules that take other photo uploads (avatar).
func NewModule(_ *sql.DB, jwt *jwtplatform.Service, s *platformstorage.Client, policies *imagepolicy.Store, audit *auditlog.Service) Module {
	return Module{handler: NewHandler(s, jwt, policies, audit)}
}

func (m Module) RegisterRoutes(r chi.Router) {
	r.Route("/storage", func(r chi.Router) {
		r.Post("/presign", m.handler.Presign)    // documents
		r.Post("/images", m.handler.UploadImage) // photos, checked against their policy
	})
	r.Get("/image-policies", m.handler.ListPolicies)
	r.Get("/admin/image-policies", m.handler.AdminListPolicies)
	r.Put("/admin/image-policies/{category}", m.handler.AdminUpdatePolicy)
	r.Get("/admin/image-policies/{category}/history", m.handler.AdminPolicyHistory)
}
