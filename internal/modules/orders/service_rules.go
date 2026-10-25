// Order input normalisation and validation.

package orders

import (
	"math"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

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
	// Every order starts PENDING; later states come from the lifecycle endpoints.
	if input.Status != "" && input.Status != "PENDING" {
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

func withLifecycle(actor ActorContext, order Order) Order {
	// Notes are the nursery's internal remarks; who handles it is internal too.
	if !sellerView(actor, order) {
		order.Notes = nil
		order.AssignedManagerUserID = nil
		order.AssignedManagerName = nil
	}
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

func totalPages(total int64, perPage int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}
