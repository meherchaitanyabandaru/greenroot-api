package orders

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/phone"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/rowversion"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden              = apperrs.ErrForbidden
	ErrInvalidInput           = apperrs.ErrInvalidInput
	ErrInvalidStatus          = errors.New("invalid status transition")
	ErrMissingDeliveryAddress = errors.New("missing delivery address")
	ErrLoadingNotDeclared     = errors.New("loading declaration required")
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

// cachedOrderList is the Global Search "Orders" category's cache-aside
// payload, keyed per-actor (see List) -- a buyer's and a seller's resolved
// scope for the same raw query differ, and even two sellers can differ
// (assigned-manager filtering), so this is never shared across users.
type cachedOrderList struct {
	Orders     []Order    `json:"orders"`
	Pagination Pagination `json:"pagination"`
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListOrdersRequest) ([]Order, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}

	if input.Search == "" {
		return s.listOrders(ctx, actor, input)
	}

	// Global Search's "Orders" category (short TTL -- orders change status
	// frequently). Key is built from the fully RBAC-resolved input (post
	// scopeList), so it already encodes exactly what this actor is allowed
	// to see.
	const module = "orders"
	redisutil.RecordSearchTerm(ctx, s.redis, nil, module, input.Search)
	gen := redisutil.SearchGeneration(ctx, s.redis, nil, module, actor.UserID)
	key := redisutil.SearchCacheKey(module, actor.UserID, gen,
		input.Search, input.Status, fmt.Sprint(input.NurseryID), fmt.Sprint(input.BuyerID),
		fmt.Sprint(input.Buying), input.SortBy, input.SortOrder,
		fmt.Sprint(input.Page), fmt.Sprint(input.PerPage))

	if cached, ok := redisutil.GetCachedSearch[cachedOrderList](ctx, s.redis, nil, module, input.Search, key); ok {
		return cached.Orders, cached.Pagination, nil
	}

	start := time.Now()
	orders, pagination, err := s.listOrders(ctx, actor, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	redisutil.SetCachedSearch(ctx, s.redis, nil, module, input.Search, key,
		cachedOrderList{Orders: orders, Pagination: pagination}, redisutil.SearchTTLOrders, time.Since(start))
	return orders, pagination, nil
}

func (s *Service) listOrders(ctx context.Context, actor ActorContext, input ListOrdersRequest) ([]Order, Pagination, error) {
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

// RenderPDF renders the order document (the same for every viewer) and
// archives it as the official copy when it changed.
func (s *Service) RenderPDF(ctx context.Context, actor ActorContext, orderID int64) ([]byte, string, error) {
	o, err := s.Get(ctx, actor, orderID)
	if err != nil {
		return nil, "", err
	}
	var nursery PDFNurseryExtras
	nurseryID := o.SellerNurseryID
	if nurseryID != nil {
		if extras, extrasErr := s.repository.GetNurseryContactExtras(ctx, *nurseryID); extrasErr == nil {
			nursery = extras
		}
	}

	// Best-effort: a token failure should never block a PDF download.
	verifyURL := s.ensureVerifyURL(ctx, orderID)

	// One document for every viewer (internal fields never print), so the
	// copy downloaded is the copy archived and verified.
	pdfBytes := buildOrderPDF(o, nursery, verifyURL)

	// Best-effort: persist it as the official historical record, deduped by
	// content hash, if storage is configured.
	s.persistDocumentIfChanged(ctx, actor, pdfBytes, o, nurseryID)

	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionDownload,
		fmt.Sprintf("Order #%d PDF generated", orderID), nil, nil)
	return pdfBytes, o.OrderCode + ".pdf", nil
}

func (s *Service) persistDocumentIfChanged(ctx context.Context, actor ActorContext, canonical []byte, o Order, nurseryID *int64) {
	if s.storage == nil {
		return
	}
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

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateOrderRequest) (Order, error) {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") || actor.HasRole("DRIVER") {
		return Order{}, ErrForbidden
	}
	if input.BuyerMobile != nil && *input.BuyerMobile != "" {
		mobile, ok := phone.Normalize(*input.BuyerMobile)
		if !ok {
			return Order{}, fmt.Errorf("%w: buyer_mobile must be a 10-digit Indian mobile number", ErrInvalidInput)
		}
		buyerID, err := s.repository.FindOrCreateBuyerByMobile(ctx, mobile, stringOrEmpty(input.BuyerName))
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
	// Empty: the repository uses the order's public code (ORD-YYYYMMDD-NNNN).
	orderNumber := strings.TrimSpace(stringOrEmpty(input.OrderNumber))
	order, err := s.repository.Create(ctx, actor.UserID, input, orderNumber)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, order.ID, auditlog.ActionCreate,
		fmt.Sprintf("Order %s created", order.OrderNumber), nil, input)
	s.enrichOrder(ctx, actor, order)
	s.bumpOrdersSearchCache(ctx, actor.UserID)
	return *order, nil
}

// bumpOrdersSearchCache invalidates the acting user's previously-cached
// Global Search "Orders" results (see List's cache-aside path). Other
// actors who can also see this order (e.g. the buyer on a seller-created
// order) fall back to the short TTL rather than a cross-actor invalidation
// fan-out -- see searchcache.go's package doc for why that's an
// intentional tradeoff, not an oversight.
func (s *Service) bumpOrdersSearchCache(ctx context.Context, userID int64) {
	redisutil.BumpSearchGeneration(ctx, s.redis, nil, "orders", userID)
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
	if err := checkIfMatch(ctx, *current); err != nil {
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
	order, err := s.repository.UpdateStatus(ctx, actor.UserID, orderID, status, current.Version)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, order.ID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d status %s → %s", orderID, current.Status, status),
		map[string]any{"status": current.Status},
		map[string]any{"status": status})
	s.enrichOrder(ctx, actor, order)
	s.bumpOrdersSearchCache(ctx, actor.UserID)
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
			_ = s.repository.CreateNotification(ctx, *driverUserID, "DELIVERY_ADDRESS_UPDATED", "Delivery Address Updated", msg, nil)
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
	if err := s.canManage(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	if err := checkIfMatch(ctx, *order); err != nil {
		return Order{}, err
	}
	if order.Status != "CONFIRMED" && order.Status != "DRAFT" {
		return Order{}, ErrInvalidStatus
	}
	if !hasUsableDeliverySnapshot(order.DeliverySnapshot) {
		return Order{}, ErrMissingDeliveryAddress
	}
	updated, err := s.repository.UpdateStatusWithLoading(ctx, actor.UserID, orderID, "LOADING", "start", order.Version)
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
// The seller must declare the vehicle carries no prohibited items; the
// declaration is kept in the audit log against who completed loading.
func (s *Service) CompleteLoading(ctx context.Context, actor ActorContext, orderID int64, declaredNoProhibitedItems bool) (Order, error) {
	if !declaredNoProhibitedItems {
		return Order{}, ErrLoadingNotDeclared
	}
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "orders", orderID)
	if err != nil {
		return Order{}, err
	}
	defer lock.Release(ctx)

	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	if err := checkIfMatch(ctx, *order); err != nil {
		return Order{}, err
	}
	if order.Status != "LOADING" {
		return Order{}, ErrInvalidStatus
	}
	finalStatus := "LOADED"
	for _, item := range order.Items {
		if item.LoadedQuantity != nil && *item.LoadedQuantity < item.Quantity {
			finalStatus = "PARTIALLY_FULFILLED"
			break
		}
	}
	updated, err := s.repository.UpdateStatusWithLoading(ctx, actor.UserID, orderID, finalStatus, "complete", order.Version)
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
		_ = s.repository.CreateNotification(ctx, *order.BuyerUserID, "ORDER_PARTIAL", "Partial Delivery Notice", msg, notifyqueue.Link("order", orderID))
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d status LOADING → %s", orderID, finalStatus),
		map[string]any{"status": "LOADING"},
		map[string]any{"status": finalStatus, "declared_no_prohibited_items": true})
	s.enrichOrder(ctx, actor, updated)
	return *updated, nil
}

// LoadingProof shows anyone who can see the order the loading photos, the
// declaration and the truck. Photos are in a private bucket: each gets a
// signed link; without storage they are left out rather than exposed.
func (s *Service) LoadingProof(ctx context.Context, actor ActorContext, orderID int64) (LoadingProof, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return LoadingProof{}, err
	}
	if err := s.canView(ctx, actor, *order); err != nil {
		return LoadingProof{}, err
	}
	proof, err := s.repository.LoadingProof(ctx, orderID)
	if err != nil {
		return LoadingProof{}, err
	}
	signed := proof.Photos[:0]
	for _, p := range proof.Photos {
		if s.storage == nil {
			break
		}
		if link, err := s.storage.PresignGetURL(ctx, p.URL, time.Hour); err == nil {
			p.URL = link
			signed = append(signed, p)
		}
	}
	proof.Photos = signed
	return proof, nil
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
	if qty > item.Quantity {
		return OrderItem{}, fmt.Errorf("%w: loaded_quantity cannot exceed the ordered quantity", ErrInvalidInput)
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
	if err := checkIfMatch(ctx, *order); err != nil {
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
	updated, err := s.repository.Cancel(ctx, actor.UserID, orderID, reason, order.Version)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d cancelled: %s", orderID, reason),
		map[string]any{"status": order.Status},
		map[string]any{"status": "CANCELLED", "reason": reason})
	s.enrichOrder(ctx, actor, updated)
	s.bumpOrdersSearchCache(ctx, actor.UserID)
	return *updated, nil
}

// AssignManager assigns a manager to an order (owner or admin only).
func (s *Service) AssignManager(ctx context.Context, actor ActorContext, orderID int64, managerUserID int64) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		nurseryID := order.SellerNurseryID
		if nurseryID == nil {
			return Order{}, ErrForbidden
		}
		owner, err := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if err != nil || !owner {
			return Order{}, ErrForbidden
		}
	}
	if IsTerminal(order.Status) {
		return Order{}, ErrInvalidStatus
	}
	if managerUserID <= 0 || order.SellerNurseryID == nil {
		return Order{}, ErrInvalidInput
	}
	if member, err := s.repository.IsNurseryMember(ctx, *order.SellerNurseryID, managerUserID); err != nil {
		return Order{}, err
	} else if !member {
		return Order{}, fmt.Errorf("%w: manager_user_id is not a member of this nursery", ErrInvalidInput)
	}
	updated, err := s.repository.AssignManager(ctx, orderID, managerUserID)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, auditlog.EntityOrder, orderID, auditlog.ActionUpdate,
		fmt.Sprintf("Order #%d manager assigned", orderID),
		map[string]any{"assigned_manager_user_id": order.AssignedManagerUserID},
		map[string]any{"assigned_manager_user_id": managerUserID})
	_ = s.repository.CreateNotification(ctx, managerUserID, "ORDER_ASSIGNED", "Order Assigned to You",
		fmt.Sprintf("You have been assigned to order %s.", updated.OrderCode), notifyqueue.Link("order", orderID))
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

// Status predicates and CanTransition are defined in policy.go.

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

// checkIfMatch enforces the client's If-Match precondition against the order
// just read. The following UPDATE is guarded by order.Version as well, so a
// change committed after this check is still caught (ErrConflict).
func checkIfMatch(ctx context.Context, order Order) error {
	if v, ok := rowversion.Expected(ctx); ok && v != order.Version {
		return ErrConflict
	}
	return nil
}
