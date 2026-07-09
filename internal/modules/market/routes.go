package market

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Module struct{ handler *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service) Module {
	repo := NewRepository(db)
	svc := NewService(repo)
	return Module{handler: NewHandler(svc, jwt)}
}

func (m Module) RegisterRoutes(r chi.Router) {
	r.Route("/market", func(r chi.Router) {
		// Browse published ads from all nurseries
		r.Get("/ads", m.handler.BrowseAds)

		// Own nursery's ads (all statuses)
		r.Get("/ads/mine", m.handler.MyAds)

		// Saved / bookmarked ads
		r.Get("/ads/saved", m.handler.SavedAds)

		// Enquiries inbox (sent + received)
		r.Get("/enquiries", m.handler.ListEnquiries)
		r.Get("/enquiries/{id}", m.handler.GetEnquiry)
		r.Post("/enquiries/{id}/reply", m.handler.ReplyToEnquiry)
		r.Post("/enquiries/{id}/close", m.handler.CloseEnquiry)
		r.Post("/enquiries/{id}/cancel", m.handler.CancelEnquiry)
		r.Post("/enquiries/{id}/link-quotation", m.handler.LinkQuotation)

		// Individual ad CRUD
		r.Post("/ads", m.handler.CreateAd)
		r.Get("/ads/{id}", m.handler.GetAd)
		r.Patch("/ads/{id}", m.handler.UpdateAd)

		// Status transitions
		r.Post("/ads/{id}/publish", m.handler.PublishAd)
		r.Post("/ads/{id}/pause", m.handler.PauseAd)
		r.Post("/ads/{id}/resume", m.handler.ResumeAd)
		r.Post("/ads/{id}/renew", m.handler.RenewAd)
		r.Post("/ads/{id}/archive", m.handler.ArchiveAd)

		// Social actions
		r.Post("/ads/{id}/save", m.handler.ToggleSave)
		r.Post("/ads/{id}/report", m.handler.ReportAd)

		// Enquiry on a specific ad
		r.Post("/ads/{id}/enquiries", m.handler.SendEnquiry)
	})
}
