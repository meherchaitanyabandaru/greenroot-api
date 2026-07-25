package orders

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden              = apperrs.ErrForbidden
	ErrInvalidInput           = apperrs.ErrInvalidInput
	ErrInvalidStatus          = errors.New("invalid status transition")
	ErrMissingDeliveryAddress = errors.New("missing delivery address")
	ErrRateLimited            = errors.New("too many requests")
	ErrDocumentNotFound       = errors.New("no verification token found for this order")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	storage    *storage.Client
	redis      redis.Cmdable
}

func NewService(repository Repository, auditSvc *auditlog.Service, storageCli *storage.Client, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: repository, auditSvc: auditSvc, storage: storageCli, redis: rdb}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListOrdersRequest) ([]Order, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}
	orders, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	s.enrichOrders(ctx, actor, orders)
	return orders, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, orderID int64) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := s.canView(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	s.enrichOrder(ctx, actor, order)
	return *order, nil
}

// RenderPDF generates the order receipt PDF on the fly (no storage persistence).
// Content is gated by role: notes and internal handler identity are hidden from
// buyers (see visibilityFor in pdf.go) — everything else is shown in full to any
// role that can view the order, since managers/owners operationally need full
// delivery details to fulfil it.
// RenderPDF generates the order receipt PDF on the fly. Content is gated by role
// (see visibilityFor) — but the QR verification link and the S3-archived "official"
// copy always reflect the full, canonical document regardless of who triggered the
// render, so history doesn't churn a new version every time a different role
// downloads the same order.
func (s *Service) RenderPDF(ctx context.Context, actor ActorContext, orderID int64) ([]byte, string, error) {
	o, err := s.Get(ctx, actor, orderID)
	if err != nil {
		return nil, "", err
	}
	var nursery PDFNurseryExtras
	nurseryID := o.NurseryID
	if nurseryID == nil {
		nurseryID = o.SellerNurseryID
	}
	if nurseryID != nil {
		if extras, extrasErr := s.repository.GetNurseryContactExtras(ctx, *nurseryID); extrasErr == nil {
			nursery = extras
		}
	}

	// Best-effort: a token failure should never block a PDF download.
	verifyURL := s.ensureVerifyURL(ctx, orderID)

	pdfBytes := buildOrderPDF(o, nursery, visibilityFor(actor), verifyURL)

	// Best-effort: persist the canonical (full-visibility) copy as the official
	// historical record, deduped by content hash, if storage is configured.
	s.persistDocumentIfChanged(ctx, actor, o, nursery, verifyURL, nurseryID)

	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionDownload,
		fmt.Sprintf("Order #%d PDF generated", orderID), nil, nil)
	return pdfBytes, o.OrderCode + ".pdf", nil
}

func (s *Service) persistDocumentIfChanged(ctx context.Context, actor ActorContext, o Order, nursery PDFNurseryExtras, verifyURL string, nurseryID *int64) {
	if s.storage == nil {
		return
	}
	canonicalVis := orderPDFVisibility{showInternalNotes: true, showAssignedHandler: true}
	canonical := buildOrderPDF(o, nursery, canonicalVis, verifyURL)
	hash := sha256.Sum256(canonical)
	hashHex := hex.EncodeToString(hash[:])

	current, err := s.repository.GetCurrentDocument(ctx, o.ID)
	if err != nil {
		return
	}
	if current != nil && current.SHA256Hash == hashHex {
		return // unchanged since last render — skip the duplicate upload
	}

	version := 1
	if current != nil {
		version = current.Version + 1
		if err := s.repository.MarkDocumentsNotCurrent(ctx, o.ID); err != nil {
			return
		}
	}

	nID := int64(0)
	if nurseryID != nil {
		nID = *nurseryID
	}
	objectKey := fmt.Sprintf("orders/%d/%d/order-v%d.pdf", nID, o.ID, version)
	if _, err := s.storage.PutObject(ctx, storage.BucketOrderPDFs, objectKey, "application/pdf", canonical); err != nil {
		return
	}

	generatedByName, _ := s.repository.GetUserName(ctx, actor.UserID)
	doc := OrderDocument{
		OrderID:         o.ID,
		Version:         version,
		ObjectKey:       objectKey,
		SHA256Hash:      hashHex,
		MimeType:        "application/pdf",
		FileSize:        int64(len(canonical)),
		GeneratedBy:     &actor.UserID,
		GeneratedByName: &generatedByName,
		IsCurrent:       true,
	}
	if _, err := s.repository.CreateDocument(ctx, doc); err != nil {
		return
	}
	s.audit(ctx, actor, auditlog.EntityOrder, o.ID, auditlog.ActionUpload,
		fmt.Sprintf("Order #%d PDF archived as version %d", o.ID, version), nil, map[string]any{
			"version": version, "sha256_hash": hashHex,
		})
}

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

// ── Verification token endpoints ─────────────────────────────────────────────

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
		nurseryID := o.NurseryID
		if nurseryID == nil {
			nurseryID = o.SellerNurseryID
		}
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

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateOrderRequest) (Order, error) {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") || actor.HasRole("DRIVER") {
		return Order{}, ErrForbidden
	}
	if input.BuyerMobile != nil && *input.BuyerMobile != "" {
		buyerID, err := s.repository.FindOrCreateBuyerByMobile(ctx, *input.BuyerMobile, stringOrEmpty(input.BuyerName))
		if err != nil {
			return Order{}, err
		}
		input.BuyerUserID = &buyerID
	}
	input = normalizeCreate(actor, input)
	if err := validateOrder(input); err != nil {
		return Order{}, err
	}
	if err := s.canCreate(ctx, actor, input); err != nil {
		return Order{}, err
	}
	orderNumber := strings.TrimSpace(stringOrEmpty(input.OrderNumber))
	if orderNumber == "" {
		orderNumber = fmt.Sprintf("GR-ORD-%d", time.Now().UnixNano())
	}
	order, err := s.repository.Create(ctx, actor.UserID, input, orderNumber)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, order.ID, auditlog.ActionCreate,
		fmt.Sprintf("Order %s created", order.OrderNumber), nil, input)
	s.enrichOrder(ctx, actor, order)
	return *order, nil
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, orderID int64, input UpdateStatusRequest) (Order, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "orders", orderID)
	if err != nil {
		return Order{}, err
	}
	defer lock.Release(ctx)

	status := strings.ToUpper(strings.TrimSpace(input.Status))
	if !AllowedStatus(status) {
		return Order{}, ErrInvalidInput
	}
	current, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := s.canManage(ctx, actor, *current); err != nil {
		return Order{}, err
	}
	if !CanTransition(current.Status, status) {
		return Order{}, ErrInvalidStatus
	}
	if status == "CONFIRMED" && !hasUsableDeliverySnapshot(current.DeliverySnapshot) {
		return Order{}, ErrMissingDeliveryAddress
	}
	if status == "COMPLETED" {
		hasUndeliveredDispatch, err := s.repository.OrderHasUndeliveredDispatch(ctx, orderID)
		if err != nil {
			return Order{}, err
		}
		if hasUndeliveredDispatch {
			return Order{}, ErrInvalidStatus
		}
	}
	order, err := s.repository.UpdateStatus(ctx, actor.UserID, orderID, status)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, order.ID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d status %s → %s", orderID, current.Status, status),
		map[string]any{"status": current.Status},
		map[string]any{"status": status})
	s.enrichOrder(ctx, actor, order)
	return *order, nil
}

func (s *Service) UpdateDeliverySnapshot(ctx context.Context, actor ActorContext, orderID int64, input DeliverySnapshotRequest) (Order, error) {
	current, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := s.canManage(ctx, actor, *current); err != nil {
		return Order{}, err
	}
	if err := validateDeliverySnapshot(input); err != nil {
		return Order{}, err
	}
	started, err := s.repository.OrderHasStartedDispatch(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if started && !input.EmergencyUpdate {
		return Order{}, ErrInvalidStatus
	}
	if input.EmergencyUpdate {
		input.LocationSource = stringPtr("admin_updated")
	}
	if _, err := s.repository.UpdateDeliverySnapshot(ctx, orderID, actor.UserID, input); err != nil {
		return Order{}, err
	}
	updated, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if input.EmergencyUpdate {
		if driverUserID, err := s.repository.StartedDispatchDriverUserID(ctx, orderID); err == nil && driverUserID != nil {
			msg := fmt.Sprintf("Delivery address changed for order %s. Please review and acknowledge the update.", updated.OrderCode)
			_ = s.repository.CreateNotification(ctx, *driverUserID, "DELIVERY_ADDRESS_UPDATED", "Delivery Address Updated", msg)
		}
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d delivery snapshot updated", orderID),
		current.DeliverySnapshot,
		updated.DeliverySnapshot)
	s.enrichOrder(ctx, actor, updated)
	return *updated, nil
}

// StartLoading marks an order as LOADING (nursery owner or assigned manager only).
func (s *Service) StartLoading(ctx context.Context, actor ActorContext, orderID int64) (Order, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "orders", orderID)
	if err != nil {
		return Order{}, err
	}
	defer lock.Release(ctx)

	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status != "CONFIRMED" && order.Status != "DRAFT" {
		return Order{}, ErrInvalidStatus
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	if !hasUsableDeliverySnapshot(order.DeliverySnapshot) {
		return Order{}, ErrMissingDeliveryAddress
	}
	updated, err := s.repository.UpdateStatusWithLoading(ctx, actor.UserID, orderID, "LOADING", "start")
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d status %s → LOADING", orderID, order.Status),
		map[string]any{"status": order.Status},
		map[string]any{"status": "LOADING"})
	s.enrichOrder(ctx, actor, updated)
	return *updated, nil
}

// CompleteLoading marks an order as LOADED or PARTIALLY_FULFILLED based on
// whether any item's loaded_quantity is less than its ordered quantity.
func (s *Service) CompleteLoading(ctx context.Context, actor ActorContext, orderID int64) (Order, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "orders", orderID)
	if err != nil {
		return Order{}, err
	}
	defer lock.Release(ctx)

	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status != "LOADING" {
		return Order{}, ErrInvalidStatus
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	finalStatus := "LOADED"
	for _, item := range order.Items {
		if item.LoadedQuantity != nil && *item.LoadedQuantity < item.Quantity {
			finalStatus = "PARTIALLY_FULFILLED"
			break
		}
	}
	updated, err := s.repository.UpdateStatusWithLoading(ctx, actor.UserID, orderID, finalStatus, "complete")
	if err != nil {
		return Order{}, err
	}
	_ = s.repository.RecalculateTotalFromLoaded(ctx, orderID)
	// Re-fetch: RecalculateTotalFromLoaded can change total_amount (partial fulfillment),
	// and `updated` above was read before that recalculation ran.
	if refetched, err := s.repository.FindByID(ctx, orderID); err == nil {
		updated = refetched
	}
	if finalStatus == "PARTIALLY_FULFILLED" && order.BuyerUserID != nil {
		msg := fmt.Sprintf("Order %s was loaded with reduced quantities. Please review your updated order.", updated.OrderCode)
		_ = s.repository.CreateNotification(ctx, *order.BuyerUserID, "ORDER_PARTIAL", "Partial Delivery Notice", msg)
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d status LOADING → %s", orderID, finalStatus),
		map[string]any{"status": "LOADING"},
		map[string]any{"status": finalStatus})
	s.enrichOrder(ctx, actor, updated)
	return *updated, nil
}

// SetLoadedQuantity records how many units were physically loaded for an item.
// Only allowed while the order is in LOADING status.
func (s *Service) SetLoadedQuantity(ctx context.Context, actor ActorContext, orderID int64, itemID int64, qty float64) (OrderItem, error) {
	if qty < 0 {
		return OrderItem{}, ErrInvalidInput
	}
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return OrderItem{}, err
	}
	if order.Status != "LOADING" {
		return OrderItem{}, ErrInvalidStatus
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return OrderItem{}, err
	}
	item, err := s.repository.FindItem(ctx, itemID)
	if err != nil {
		return OrderItem{}, err
	}
	if item.OrderID != orderID {
		return OrderItem{}, ErrForbidden
	}
	updated, err := s.repository.SetLoadedQuantity(ctx, itemID, qty)
	if err != nil {
		return OrderItem{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrderItem, itemID, auditlog.ActionUpdate,
		fmt.Sprintf("Order item #%d loaded quantity → %.2f", itemID, qty),
		map[string]any{"loaded_quantity": item.LoadedQuantity},
		map[string]any{"loaded_quantity": qty})
	return *updated, nil
}

// Cancel cancels an order.
func (s *Service) Cancel(ctx context.Context, actor ActorContext, orderID int64, reason string) (Order, error) {
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "orders", orderID)
	if err != nil {
		return Order{}, err
	}
	defer lock.Release(ctx)

	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status == "CANCELLED" || order.Status == "COMPLETED" || order.Status == "LOADED" || order.Status == "PARTIALLY_FULFILLED" {
		return Order{}, ErrInvalidStatus
	}
	// Buyer may cancel their own PENDING order; all other cancellations require nursery management access.
	isBuyerSelfCancel := order.BuyerUserID != nil && *order.BuyerUserID == actor.UserID && order.Status == "PENDING"
	if !isBuyerSelfCancel {
		if err := s.canManage(ctx, actor, *order); err != nil {
			return Order{}, err
		}
	}
	updated, err := s.repository.Cancel(ctx, actor.UserID, orderID, reason)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d cancelled: %s", orderID, reason),
		map[string]any{"status": order.Status},
		map[string]any{"status": "CANCELLED", "reason": reason})
	s.enrichOrder(ctx, actor, updated)
	return *updated, nil
}

// AssignManager assigns a manager to an order (owner or admin only).
func (s *Service) AssignManager(ctx context.Context, actor ActorContext, orderID int64, managerUserID int64) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		nurseryID := order.NurseryID
		if nurseryID == nil {
			nurseryID = order.SellerNurseryID
		}
		if nurseryID == nil {
			return Order{}, ErrForbidden
		}
		owner, err := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if err != nil || !owner {
			return Order{}, ErrForbidden
		}
	}
	updated, err := s.repository.AssignManager(ctx, orderID, managerUserID)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d manager assigned", orderID),
		map[string]any{"assigned_manager_user_id": order.AssignedManagerUserID},
		map[string]any{"assigned_manager_user_id": managerUserID})
	s.enrichOrder(ctx, actor, updated)
	return *updated, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, orderID int64) error {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return err
	}
	if order.Status != "PENDING" {
		return ErrInvalidStatus
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, orderID); err != nil {
		return err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionDelete,
		fmt.Sprintf("Order #%d deleted", orderID),
		map[string]any{"status": order.Status}, nil)
	return nil
}

func (s *Service) ListItems(ctx context.Context, actor ActorContext, orderID int64) ([]OrderItem, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if err := s.canView(ctx, actor, *order); err != nil {
		return nil, err
	}
	return s.repository.ListItems(ctx, orderID)
}

func (s *Service) CreateItem(ctx context.Context, actor ActorContext, orderID int64, input OrderItemRequest) (OrderItem, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return OrderItem{}, err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return OrderItem{}, err
	}
	if !IsEditable(order.Status) {
		return OrderItem{}, ErrInvalidStatus
	}
	if err := validateItem(input); err != nil {
		return OrderItem{}, err
	}
	item, err := s.repository.CreateItem(ctx, orderID, input)
	if err != nil {
		return OrderItem{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrderItem, item.ID, auditlog.ActionCreate,
		fmt.Sprintf("Item added to order #%d", orderID),
		nil, input)
	return *item, nil
}

func (s *Service) UpdateItem(ctx context.Context, actor ActorContext, itemID int64, input OrderItemRequest) (OrderItem, error) {
	if err := validateItem(input); err != nil {
		return OrderItem{}, err
	}
	current, err := s.repository.FindItem(ctx, itemID)
	if err != nil {
		return OrderItem{}, err
	}
	order, err := s.repository.FindByID(ctx, current.OrderID)
	if err != nil {
		return OrderItem{}, err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return OrderItem{}, err
	}
	if !IsEditable(order.Status) {
		return OrderItem{}, ErrInvalidStatus
	}
	item, err := s.repository.UpdateItem(ctx, itemID, input)
	if err != nil {
		return OrderItem{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrderItem, item.ID, auditlog.ActionUpdate,
		fmt.Sprintf("Order item #%d updated", item.ID),
		current, input)
	return *item, nil
}

func (s *Service) DeleteItem(ctx context.Context, actor ActorContext, itemID int64) error {
	item, err := s.repository.FindItem(ctx, itemID)
	if err != nil {
		return err
	}
	order, err := s.repository.FindByID(ctx, item.OrderID)
	if err != nil {
		return err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return err
	}
	if !IsEditable(order.Status) {
		return ErrInvalidStatus
	}
	if err := s.repository.DeleteItem(ctx, itemID); err != nil {
		return err
	}
	s.audit(ctx, actor, auditlog.EntityOrderItem, itemID, auditlog.ActionDelete,
		fmt.Sprintf("Order item #%d removed from order #%d", itemID, item.OrderID),
		item, nil)
	return nil
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListOrdersRequest) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if input.Buying {
		// Buyer perspective: filter by buyer_user_id OR buyer_nursery_id
		input.BuyerID = actor.UserID
		if actor.HasRole("NURSERY_OWNER") {
			nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
			if nurseryID != nil {
				input.NurseryID = *nurseryID
			}
		}
		return nil
	}
	if actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") {
		if input.NurseryID > 0 {
			return s.mustBeNurseryMember(ctx, actor, input.NurseryID)
		}
		nurseryIDs, err := s.repository.GetUserNurseryIDs(ctx, actor.UserID)
		if err != nil {
			return err
		}
		if len(nurseryIDs) == 0 {
			return ErrForbidden
		}
		input.NurseryID = nurseryIDs[0]
		return nil
	}
	if actor.HasRole("DRIVER") {
		return ErrForbidden
	}
	input.BuyerID = actor.UserID
	return nil
}

func (s *Service) canView(ctx context.Context, actor ActorContext, order Order) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if order.CustomerUserID != nil && *order.CustomerUserID == actor.UserID {
		return nil
	}
	if order.BuyerUserID != nil && *order.BuyerUserID == actor.UserID {
		return nil
	}
	if order.BuyerNurseryID != nil {
		isOwner, _ := s.repository.IsNurseryOwner(ctx, *order.BuyerNurseryID, actor.UserID)
		if isOwner {
			return nil
		}
	}
	if order.AssignedManagerUserID != nil && *order.AssignedManagerUserID == actor.UserID {
		return nil
	}
	nurseryID := order.NurseryID
	if nurseryID == nil {
		nurseryID = order.SellerNurseryID
	}
	if nurseryID != nil {
		owner, _ := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if owner {
			return nil
		}
		if err := s.mustBeNurseryMember(ctx, actor, *nurseryID); err == nil {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) canCreate(ctx context.Context, actor ActorContext, input CreateOrderRequest) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return ErrForbidden
	}
	if actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") {
		if input.SellerNurseryID == nil {
			return ErrInvalidInput
		}
		return s.mustBeNurseryMember(ctx, actor, *input.SellerNurseryID)
	}
	// BUYER creating own order
	if input.BuyerUserID != nil && *input.BuyerUserID != actor.UserID {
		return ErrForbidden
	}
	return nil
}

func (s *Service) canManage(ctx context.Context, actor ActorContext, order Order) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if order.AssignedManagerUserID != nil && *order.AssignedManagerUserID == actor.UserID {
		return nil
	}
	nurseryID := order.NurseryID
	if nurseryID == nil {
		nurseryID = order.SellerNurseryID
	}
	if nurseryID != nil {
		owner, _ := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if owner {
			return nil
		}
		if err := s.mustBeNurseryMember(ctx, actor, *nurseryID); err == nil {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) mustBeNurseryMember(ctx context.Context, actor ActorContext, nurseryID int64) error {
	member, err := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if err != nil {
		return err
	}
	if !member {
		return ErrForbidden
	}
	return nil
}

func normalizeList(input ListOrdersRequest) ListOrdersRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Search = strings.TrimSpace(input.Search)
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = "desc"
	}
	return input
}

func normalizeCreate(actor ActorContext, input CreateOrderRequest) CreateOrderRequest {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "PENDING"
	}
	// Only default buyer to self when a BUYER is creating their own order
	if input.BuyerUserID == nil && actor.HasRole("BUYER") {
		input.BuyerUserID = &actor.UserID
	}
	return input
}

func validateOrder(input CreateOrderRequest) error {
	if input.BuyerUserID == nil || *input.BuyerUserID <= 0 || input.SellerNurseryID == nil || *input.SellerNurseryID <= 0 {
		return ErrInvalidInput
	}
	if !AllowedStatus(input.Status) {
		return ErrInvalidInput
	}
	for _, item := range input.Items {
		if err := validateItem(item); err != nil {
			return err
		}
	}
	if input.Delivery != nil {
		if err := validateDeliverySnapshot(*input.Delivery); err != nil {
			return err
		}
	}
	return nil
}

func validateDeliverySnapshot(input DeliverySnapshotRequest) error {
	if input.Latitude != nil && input.Longitude == nil {
		return ErrInvalidInput
	}
	if input.Longitude != nil && input.Latitude == nil {
		return ErrInvalidInput
	}
	if input.Latitude != nil && (*input.Latitude < -90 || *input.Latitude > 90) {
		return ErrInvalidInput
	}
	if input.Longitude != nil && (*input.Longitude < -180 || *input.Longitude > 180) {
		return ErrInvalidInput
	}
	if input.GPSAccuracyM != nil && *input.GPSAccuracyM < 0 {
		return ErrInvalidInput
	}
	if input.LocationSource != nil && !isAllowedLocationSource(*input.LocationSource) {
		return ErrInvalidInput
	}
	return nil
}

func hasUsableDeliverySnapshot(snapshot *DeliverySnapshot) bool {
	if snapshot == nil || snapshot.AddressLine1 == nil {
		return false
	}
	return strings.TrimSpace(*snapshot.AddressLine1) != ""
}

func isAllowedLocationSource(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "gps_confirmed", "nursery_default", "map_selected", "address_search", "admin_updated":
		return true
	default:
		return false
	}
}

func stringPtr(value string) *string {
	return &value
}

func (s *Service) enrichOrders(ctx context.Context, actor ActorContext, orders []Order) {
	if len(orders) == 0 {
		return
	}
	ids := make([]int64, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	dispatches, _ := s.repository.BatchActiveDispatchForOrders(ctx, ids)
	for i := range orders {
		if dispatches != nil {
			if d := dispatches[orders[i].ID]; d != nil {
				orders[i].ActiveDispatch = d
				orders[i].ActiveDispatchID = &d.ID
				orders[i].ActiveDispatchStatus = &d.Status
			}
		}
		orders[i] = withLifecycle(actor, orders[i])
	}
}

func (s *Service) enrichOrder(ctx context.Context, actor ActorContext, order *Order) {
	if order == nil {
		return
	}
	if active, err := s.repository.ActiveDispatchForOrder(ctx, order.ID); err == nil && active != nil {
		order.ActiveDispatch = active
		order.ActiveDispatchID = &active.ID
		order.ActiveDispatchStatus = &active.Status
	}
	*order = withLifecycle(actor, *order)
}

func withLifecycle(actor ActorContext, order Order) Order {
	lc := lifecycle.Order(order.Status)
	if order.ActiveDispatchStatus != nil {
		lc = lifecycle.OrderWithDispatch(order.Status, *order.ActiveDispatchStatus)
	}
	order.Lifecycle = &lc
	caps := BuildCapabilities(actor, order)
	order.Capabilities = &caps
	return order
}

func validateItem(input OrderItemRequest) error {
	if input.PlantID <= 0 || input.Quantity <= 0 || input.UnitPrice < 0 || input.TotalPrice < 0 {
		return ErrInvalidInput
	}
	expected := input.Quantity * input.UnitPrice
	if math.Abs(expected-input.TotalPrice) > 0.01 {
		return ErrInvalidInput
	}
	return nil
}

// Status predicates and CanTransition are defined in policy.go.

func totalPages(total int64, perPage int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, description string, oldValue, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:      actor.UserID,
		Module:      auditlog.ModuleOrders,
		EntityType:  entityType,
		EntityID:    entityID,
		Action:      action,
		Description: description,
		OldValue:    oldValue,
		NewValue:    newValue,
		IPAddress:   actor.IPAddress,
		DeviceInfo:  actor.UserAgent,
	})
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
