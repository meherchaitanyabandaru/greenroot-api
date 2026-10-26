package reports

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Handler struct {
	service *Service
	jwt     *jwtplatform.Service
}

func NewHandler(service *Service, jwt *jwtplatform.Service) *Handler {
	return &Handler{service: service, jwt: jwt}
}

// Get godoc: GET /reports/{type}?from=YYYY-MM-DD&to=YYYY-MM-DD[&format=csv]
// [&page&per_page][&view=buying][&nursery_id] (admin only)
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return
	}
	req, err := ParseRequest(chi.URLParam(r, "type"), r.URL.Query())
	if err != nil {
		writeError(w, err)
		return
	}
	sc, err := h.service.ResolveScope(r.Context(), actor.AsActorContext(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	if req.CSV {
		name := "greenroot-" + req.Type + "-" + req.From.Format("2006-01-02") + "-to-" +
			req.ToExcl.AddDate(0, 0, -1).Format("2006-01-02") + ".csv"
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "no-store")
		// The status is already sent once rows stream: a mid-export failure
		// can only cut the file short, so it is logged.
		if err := h.service.repo.StreamCSV(r.Context(), req, sc, w); err != nil {
			slog.ErrorContext(r.Context(), "report csv export failed", "type", req.Type, "error", err)
		}
		return
	}
	report, err := h.service.Report(r.Context(), req, sc)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, report)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidType):
		response.Error(w, http.StatusBadRequest, "invalid_input", "report type must be orders, quotations or deliveries")
	case errors.Is(err, ErrInvalidRange):
		response.Error(w, http.StatusBadRequest, "invalid_input", "from and to must be dates (YYYY-MM-DD), from not after to, at most 3 years apart")
	case errors.Is(err, ErrInvalidFormat):
		response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "this report is not available to you")
	default:
		response.Error(w, http.StatusInternalServerError, "internal_error", "could not build the report")
	}
}
