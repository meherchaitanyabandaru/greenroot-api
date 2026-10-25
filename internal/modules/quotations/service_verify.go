// Public verification links for quotations: tokens, the public verify page and its per-IP limit.

package quotations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
)

type ipRateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

func newIPRateLimiter(window time.Duration, max int) *ipRateLimiter {
	rl := &ipRateLimiter{hits: make(map[string][]time.Time), window: window, max: max}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *ipRateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)
	hits := rl.hits[ip]
	valid := hits[:0]
	for _, h := range hits {
		if h.After(cutoff) {
			valid = append(valid, h)
		}
	}
	if len(valid) >= rl.max {
		rl.hits[ip] = valid
		return false
	}
	rl.hits[ip] = append(valid, now)
	return true
}

func (rl *ipRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.window)
	for k, hits := range rl.hits {
		valid := hits[:0]
		for _, h := range hits {
			if h.After(cutoff) {
				valid = append(valid, h)
			}
		}
		if len(valid) == 0 {
			delete(rl.hits, k)
		} else {
			rl.hits[k] = valid
		}
	}
}

// publicVerifyLimiter: 30 requests per IP per 10 minutes.
var publicVerifyLimiter = newIPRateLimiter(10*time.Minute, 30)

// GetOrCreateVerifyToken returns the existing ACTIVE token for a quotation.
// Owner and manager may also create one if none exists. Admin and buyer can
// only read an existing token — they receive ErrDocumentNotFound if none exists.
func (s *Service) GetOrCreateVerifyToken(ctx context.Context, actor ActorContext, quotationID int64) (VerifyTokenResponse, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return VerifyTokenResponse{}, err
	}

	canCreate := s.canManage(ctx, actor, *q) == nil
	if !canCreate {
		if err := s.canView(ctx, actor, *q); err != nil {
			return VerifyTokenResponse{}, ErrForbidden
		}
	}

	existing, err := s.repository.GetActiveVerificationToken(ctx, quotationID)
	if err != nil {
		return VerifyTokenResponse{}, err
	}
	if existing != nil {
		return VerifyTokenResponse{
			Token:     existing.Token,
			VerifyURL: s.verifyURL(existing.Token),
			CreatedAt: existing.CreatedAt,
		}, nil
	}

	if !canCreate {
		return VerifyTokenResponse{}, ErrDocumentNotFound
	}

	token, err := generateToken()
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("generate token: %w", err)
	}
	created, err := s.repository.CreateVerificationToken(ctx, quotationID, token)
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("store token: %w", err)
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, auditlog.ActionCreate,
		map[string]any{"event": "verify_token_created", "token_prefix": token[:8]})
	return VerifyTokenResponse{
		Token:     created.Token,
		VerifyURL: s.verifyURL(created.Token),
		CreatedAt: created.CreatedAt,
	}, nil
}

// RevokeAndRegenerateToken revokes the current active token and issues a new one.
// Only the nursery owner may call this.
func (s *Service) RevokeAndRegenerateToken(ctx context.Context, actor ActorContext, quotationID int64) (VerifyTokenResponse, error) {
	q, err := s.repository.FindByID(ctx, quotationID)
	if err != nil {
		return VerifyTokenResponse{}, err
	}
	if !s.isNurseryOwner(ctx, actor, *q) {
		return VerifyTokenResponse{}, ErrForbidden
	}

	if err := s.repository.RevokeVerificationTokens(ctx, quotationID, actor.UserID); err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("revoke token: %w", err)
	}
	token, err := generateToken()
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("generate token: %w", err)
	}
	created, err := s.repository.CreateVerificationToken(ctx, quotationID, token)
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("store token: %w", err)
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, quotationID, auditlog.ActionUpdate,
		map[string]any{"event": "verify_token_revoked_and_regenerated", "token_prefix": token[:8]})
	return VerifyTokenResponse{
		Token:     created.Token,
		VerifyURL: s.verifyURL(created.Token),
		CreatedAt: created.CreatedAt,
	}, nil
}

// PublicVerify is the unauthenticated endpoint for QR code scans.
// Returns only safe public fields — never reveals nursery name, customer, prices, or hash.
func (s *Service) PublicVerify(ctx context.Context, token string, remoteIP string) (PublicVerifyResponse, error) {
	if !publicVerifyLimiter.Allow(remoteIP) {
		return PublicVerifyResponse{}, ErrRateLimited
	}

	v, err := s.repository.GetVerificationByToken(ctx, token)
	if err != nil {
		return PublicVerifyResponse{}, err
	}

	now := time.Now()

	// Audit the scan (no user ID — public access).
	if s.auditSvc != nil {
		s.auditSvc.Log(ctx, auditlog.Entry{
			Module:     auditlog.ModuleQuotations,
			EntityType: auditlog.EntityQuotation,
			Action:     "QR_SCAN",
			IPAddress:  remoteIP,
			NewValue:   map[string]any{"token_prefix": token[:min(8, len(token))], "found": v != nil},
		})
	}

	if v == nil || v.Status == "REVOKED" {
		return PublicVerifyResponse{
			Authenticity:      "INVALID",
			QuotationStatus:   "UNKNOWN",
			DocumentIntegrity: "UNVERIFIED",
			VerifiedAt:        now,
		}, nil
	}

	q, err := s.repository.FindByID(ctx, v.QuotationID)
	if err != nil {
		return PublicVerifyResponse{}, err
	}

	// Determine quotation status (offer state, independent of authenticity).
	quotationStatus := publicQuotationStatus(*q)

	// Document integrity: check if an official PDF has been stored.
	doc, _ := s.repository.GetCurrentDocument(ctx, v.QuotationID)
	documentIntegrity := "UNVERIFIED"
	if doc != nil {
		documentIntegrity = "UNMODIFIED"
	}

	return PublicVerifyResponse{
		QuotationCode:     q.QuotationCode,
		Authenticity:      "VERIFIED",
		QuotationStatus:   quotationStatus,
		DocumentIntegrity: documentIntegrity,
		IssuedAt:          q.CreatedAt,
		ValidUntil:        q.ValidUntil,
		VerifiedAt:        now,
	}, nil
}

// GetByToken returns the full quotation for authenticated users who are the
// nursery owner (seller) or the customer (buyer). All other roles get 403.
func (s *Service) GetByToken(ctx context.Context, actor ActorContext, token string) (Quotation, error) {
	v, err := s.repository.GetVerificationByToken(ctx, token)
	if err != nil {
		return Quotation{}, err
	}
	if v == nil {
		return Quotation{}, ErrNotFound
	}

	q, err := s.repository.FindByID(ctx, v.QuotationID)
	if err != nil {
		return Quotation{}, err
	}

	// Strict two-party RBAC: only the nursery owner (seller) or the buyer.
	isOwner := s.isNurseryOwner(ctx, actor, *q)
	isBuyer := q.CustomerUserID != nil && *q.CustomerUserID == actor.UserID
	if !isOwner && !isBuyer {
		s.audit(ctx, actor, auditlog.EntityQuotation, q.ID, "UNAUTHORIZED_VIEW_ATTEMPT",
			map[string]any{"token_prefix": token[:min(8, len(token))]})
		return Quotation{}, ErrForbidden
	}

	s.audit(ctx, actor, auditlog.EntityQuotation, q.ID, auditlog.ActionDownload,
		map[string]any{"event": "full_view_by_token"})
	return enrichQuotation(actor, *q), nil
}

// webBaseURL is the public web app origin used to build scannable verification links.
// Overridable via WEB_BASE_URL for non-production environments.
var webBaseURL = envOr("WEB_BASE_URL", "https://greenroot.app")

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Path must be /verify/<token> — the mobile app's QR classifier only recognizes
// a bare 64-hex token or a URL containing this exact segment (see classifier.dart).
func (s *Service) verifyURL(token string) string {
	return strings.TrimRight(webBaseURL, "/") + "/verify/" + token
}

func generateToken() (string, error) {
	b := make([]byte, 32) // 256-bit entropy
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// publicQuotationStatus maps the internal quotation status to the public-facing label.
// "Expired" means the offer window has passed — not that the document is invalid.
func publicQuotationStatus(q Quotation) string {
	switch q.Status {
	case "CUSTOMER_SENT":
		if q.ValidUntil != nil && time.Now().After(*q.ValidUntil) {
			return "EXPIRED"
		}
		return "ACTIVE"
	case "CUSTOMER_ACCEPTED":
		return "ACTIVE"
	case "CUSTOMER_REJECTED", "INTERNAL_DRAFT", "CUSTOMER_DRAFT":
		return "CANCELLED"
	case "CONVERTED":
		return "CONVERTED"
	default:
		return "CANCELLED"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
