// Public verification links for orders: tokens, the public verify page and its per-IP limit.
package orders

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

// ensureVerifyURL returns the URL for the order's active verification token,
// auto-provisioning one if none exists yet. Best-effort — errors return "" so a
// verification failure never blocks a PDF render.
func (s *Service) ensureVerifyURL(ctx context.Context, orderID int64) string {
	existing, err := s.repository.GetActiveVerificationToken(ctx, orderID)
	if err == nil && existing != nil {
		return s.verifyURL(existing.Token)
	}
	token, err := generateOrderToken()
	if err != nil {
		return ""
	}
	created, err := s.repository.CreateVerificationToken(ctx, orderID, token)
	if err != nil {
		return ""
	}
	return s.verifyURL(created.Token)
}

// GetOrCreateVerifyToken returns the existing ACTIVE token for an order, creating
// one if the actor can manage the order. Buyers/admins may only read an existing
// token — they receive ErrDocumentNotFound if none exists yet.
func (s *Service) GetOrCreateVerifyToken(ctx context.Context, actor ActorContext, orderID int64) (VerifyTokenResponse, error) {
	o, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return VerifyTokenResponse{}, err
	}
	canCreate := s.canManage(ctx, actor, *o) == nil
	if !canCreate {
		if err := s.canView(ctx, actor, *o); err != nil {
			return VerifyTokenResponse{}, ErrForbidden
		}
	}

	existing, err := s.repository.GetActiveVerificationToken(ctx, orderID)
	if err != nil {
		return VerifyTokenResponse{}, err
	}
	if existing != nil {
		return VerifyTokenResponse{Token: existing.Token, VerifyURL: s.verifyURL(existing.Token), CreatedAt: existing.CreatedAt}, nil
	}
	if !canCreate {
		return VerifyTokenResponse{}, ErrDocumentNotFound
	}

	token, err := generateOrderToken()
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("generate token: %w", err)
	}
	created, err := s.repository.CreateVerificationToken(ctx, orderID, token)
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("store token: %w", err)
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionCreate,
		fmt.Sprintf("Order #%d verify token created", orderID), nil, map[string]any{"token_prefix": token[:8]})
	return VerifyTokenResponse{Token: created.Token, VerifyURL: s.verifyURL(created.Token), CreatedAt: created.CreatedAt}, nil
}

// RevokeAndRegenerateToken revokes the current active token and issues a new one.
// Only the nursery owner (or admin) may call this.
func (s *Service) RevokeAndRegenerateToken(ctx context.Context, actor ActorContext, orderID int64) (VerifyTokenResponse, error) {
	o, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return VerifyTokenResponse{}, err
	}
	isAdmin := actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
	if !isAdmin {
		nurseryID := o.SellerNurseryID
		if nurseryID == nil {
			return VerifyTokenResponse{}, ErrForbidden
		}
		owner, err := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if err != nil || !owner {
			return VerifyTokenResponse{}, ErrForbidden
		}
	}

	if err := s.repository.RevokeVerificationTokens(ctx, orderID, actor.UserID); err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("revoke token: %w", err)
	}
	token, err := generateOrderToken()
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("generate token: %w", err)
	}
	created, err := s.repository.CreateVerificationToken(ctx, orderID, token)
	if err != nil {
		return VerifyTokenResponse{}, fmt.Errorf("store token: %w", err)
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d verify token revoked and regenerated", orderID), nil, map[string]any{"token_prefix": token[:8]})
	return VerifyTokenResponse{Token: created.Token, VerifyURL: s.verifyURL(created.Token), CreatedAt: created.CreatedAt}, nil
}

// publicVerifyOrderLimiter: 30 requests per IP per 10 minutes.
var publicVerifyOrderLimiter = newOrderIPRateLimiter(10*time.Minute, 30)

// PublicVerify is the unauthenticated endpoint for QR code scans. Returns only
// safe public fields — never reveals nursery name, buyer details, prices, or hash.
func (s *Service) PublicVerify(ctx context.Context, token string, remoteIP string) (PublicVerifyResponse, error) {
	if !publicVerifyOrderLimiter.Allow(remoteIP) {
		return PublicVerifyResponse{}, ErrRateLimited
	}

	v, err := s.repository.GetVerificationByToken(ctx, token)
	if err != nil {
		return PublicVerifyResponse{}, err
	}

	now := time.Now()
	if s.auditSvc != nil {
		s.auditSvc.Log(ctx, auditlog.Entry{
			Module:     auditlog.ModuleOrders,
			EntityType: auditlog.EntityOrder,
			Action:     "QR_SCAN",
			IPAddress:  remoteIP,
			NewValue:   map[string]any{"token_prefix": token[:min(8, len(token))], "found": v != nil},
		})
	}

	if v == nil || v.Status == "REVOKED" {
		return PublicVerifyResponse{Authenticity: "INVALID", OrderStatus: "UNKNOWN", DocumentIntegrity: "UNVERIFIED", VerifiedAt: now}, nil
	}

	o, err := s.repository.FindByID(ctx, v.OrderID)
	if err != nil {
		return PublicVerifyResponse{}, err
	}

	doc, _ := s.repository.GetCurrentDocument(ctx, v.OrderID)
	documentIntegrity := "UNVERIFIED"
	if doc != nil {
		documentIntegrity = "UNMODIFIED"
	}

	return PublicVerifyResponse{
		OrderCode:         o.OrderCode,
		Authenticity:      "VERIFIED",
		OrderStatus:       o.Status,
		DocumentIntegrity: documentIntegrity,
		IssuedAt:          o.CreatedAt,
		VerifiedAt:        now,
	}, nil
}

// webBaseURL is the public web app origin used to build scannable verification links.
var orderWebBaseURL = envOr("WEB_BASE_URL", "https://greenroot.app")

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Path is /verify-order/<token> — distinct from quotations' /verify/<token> so the
// two public routes never collide; the mobile QR classifier recognizes both.
func (s *Service) verifyURL(token string) string {
	return strings.TrimRight(orderWebBaseURL, "/") + "/verify-order/" + token
}

func generateOrderToken() (string, error) {
	b := make([]byte, 32) // 256-bit entropy
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type orderIPRateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

func newOrderIPRateLimiter(window time.Duration, max int) *orderIPRateLimiter {
	rl := &orderIPRateLimiter{hits: make(map[string][]time.Time), window: window, max: max}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *orderIPRateLimiter) Allow(ip string) bool {
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

func (rl *orderIPRateLimiter) cleanup() {
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
