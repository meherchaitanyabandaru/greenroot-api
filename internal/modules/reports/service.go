package reports

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

var (
	ErrInvalidType   = errors.New("unknown report type")
	ErrInvalidRange  = errors.New("invalid date range")
	ErrInvalidFormat = errors.New("format must be json or csv")
	ErrForbidden     = errors.New("forbidden")
)

// india is the business time zone: report days start at 00:00 IST.
var india = time.FixedZone("IST", 5*60*60+30*60)

type store interface {
	OwnedNurseryID(ctx context.Context, userID int64) (int64, error)
	MemberNurseryID(ctx context.Context, userID int64) (int64, error)
	Summary(ctx context.Context, req Request, sc Scope) (Summary, error)
	Rows(ctx context.Context, req Request, sc Scope, limit, offset int) ([]Row, error)
	StreamCSV(ctx context.Context, req Request, sc Scope, w io.Writer) error
}

type Service struct {
	repo store
}

func NewService(repo store) *Service { return &Service{repo: repo} }

// ParseRequest validates the report type, dates (YYYY-MM-DD, from <= to, at
// most three years apart), format and paging.
func ParseRequest(reportType string, q url.Values) (Request, error) {
	req := Request{Type: reportType}
	switch reportType {
	case TypeOrders, TypeQuotations, TypeDeliveries:
	default:
		return req, ErrInvalidType
	}
	from, err1 := time.ParseInLocation("2006-01-02", q.Get("from"), india)
	to, err2 := time.ParseInLocation("2006-01-02", q.Get("to"), india)
	if err1 != nil || err2 != nil || to.Before(from) || to.After(from.AddDate(MaxRangeYears, 0, 0)) {
		return req, ErrInvalidRange
	}
	req.From, req.ToExcl = from, to.AddDate(0, 0, 1)

	switch q.Get("format") {
	case "", "json":
	case "csv":
		req.CSV = true
	default:
		return req, ErrInvalidFormat
	}
	req.Page, _ = strconv.Atoi(q.Get("page"))
	if req.Page < 1 {
		req.Page = 1
	}
	req.PerPage, _ = strconv.Atoi(q.Get("per_page"))
	if req.PerPage < 1 || req.PerPage > 100 {
		req.PerPage = 20
	}
	req.Buying = q.Get("view") == "buying"
	req.Nursery, _ = strconv.ParseInt(q.Get("nursery_id"), 10, 64)
	return req, nil
}

// ResolveScope decides whose rows the caller may read. The nursery comes from
// the database (as in the orders module), never from the request, except an
// admin's optional filter.
func (s *Service) ResolveScope(ctx context.Context, actor authctx.ActorContext, req Request) (Scope, error) {
	sc := Scope{UserID: actor.UserID}
	switch {
	case req.Buying:
		sc.Kind = ScopeBuyer
	case actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN"):
		sc.Kind, sc.NurseryID = ScopeAll, req.Nursery
	case actor.HasRole("NURSERY_OWNER"):
		id, err := s.repo.OwnedNurseryID(ctx, actor.UserID)
		if err != nil {
			return sc, err
		}
		if id == 0 {
			return sc, ErrForbidden
		}
		sc.Kind, sc.NurseryID = ScopeNursery, id
	case actor.HasRole("MANAGER"):
		id, err := s.repo.MemberNurseryID(ctx, actor.UserID)
		if err != nil {
			return sc, err
		}
		if id == 0 {
			return sc, ErrForbidden
		}
		sc.Kind, sc.NurseryID = ScopeManager, id
	case actor.HasRole("DRIVER"):
		sc.Kind = ScopeDriver
	default:
		sc.Kind = ScopeBuyer
	}
	// A driver's work is trips; they have no orders or quotations of their own.
	if sc.Kind == ScopeDriver && req.Type != TypeDeliveries {
		return sc, ErrForbidden
	}
	return sc, nil
}

// Report returns the summary and one page of rows.
func (s *Service) Report(ctx context.Context, req Request, sc Scope) (Report, error) {
	sum, err := s.repo.Summary(ctx, req, sc)
	if err != nil {
		return Report{}, err
	}
	rows, err := s.repo.Rows(ctx, req, sc, req.PerPage, (req.Page-1)*req.PerPage)
	if err != nil {
		return Report{}, err
	}
	pages := int((sum.Total + int64(req.PerPage) - 1) / int64(req.PerPage))
	return Report{
		Type:    req.Type,
		From:    req.From.Format("2006-01-02"),
		To:      req.ToExcl.AddDate(0, 0, -1).Format("2006-01-02"),
		Summary: sum,
		Rows:    rows,
		Pagination: response.Pagination{
			Page: req.Page, PerPage: req.PerPage, Total: sum.Total, TotalPages: pages,
		},
	}, nil
}

// MaskMobile keeps the first two and last three digits: 98xxxxx210.
func MaskMobile(m string) string {
	d := strings.TrimSpace(m)
	if len(d) < 6 {
		return strings.Repeat("x", len(d))
	}
	return d[:2] + strings.Repeat("x", len(d)-5) + d[len(d)-3:]
}

// csvSafe stops a spreadsheet from running a cell as a formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}
