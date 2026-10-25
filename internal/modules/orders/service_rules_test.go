package orders

import (
	"errors"
	"testing"
)

func f64(v float64) *float64 { return &v }

func TestNormalizeList(t *testing.T) {
	got := normalizeList(ListOrdersRequest{Search: "  rose ", Status: " confirmed ", SortOrder: "ASC"})
	if got.Page != 1 || got.PerPage != 20 || got.Search != "rose" || got.Status != "CONFIRMED" || got.SortOrder != "asc" {
		t.Errorf("defaults/trim: %+v", got)
	}
	if got := normalizeList(ListOrdersRequest{PerPage: 500, SortOrder: "sideways"}); got.PerPage != 100 || got.SortOrder != "desc" {
		t.Errorf("caps per_page at 100 and defaults sort to desc: %+v", got)
	}
}

func TestNormalizeCreate_BuyerDefaultsToSelf(t *testing.T) {
	buyer := ActorContext{UserID: 7, Roles: []string{"BUYER"}}
	if got := normalizeCreate(buyer, CreateOrderRequest{Status: " pending "}); got.BuyerUserID == nil || *got.BuyerUserID != 7 || got.Status != "PENDING" {
		t.Errorf("buyer: %+v", got)
	}
	owner := ActorContext{UserID: 8, Roles: []string{"NURSERY_OWNER"}}
	if got := normalizeCreate(owner, CreateOrderRequest{}); got.BuyerUserID != nil {
		t.Errorf("an owner must name the buyer, got %v", *got.BuyerUserID)
	}
}

func TestValidateOrder(t *testing.T) {
	one := int64(1)
	ok := CreateOrderRequest{BuyerUserID: &one, SellerNurseryID: &one}
	if err := validateOrder(ok); err != nil {
		t.Fatalf("valid order refused: %v", err)
	}
	bad := map[string]CreateOrderRequest{
		"no buyer":     {SellerNurseryID: &one},
		"no seller":    {BuyerUserID: &one},
		"not PENDING":  {BuyerUserID: &one, SellerNurseryID: &one, Status: "CONFIRMED"},
		"bad item":     {BuyerUserID: &one, SellerNurseryID: &one, Items: []OrderItemRequest{{PlantID: 1, Quantity: 0}}},
		"bad delivery": {BuyerUserID: &one, SellerNurseryID: &one, Delivery: &DeliverySnapshotRequest{Latitude: f64(10)}},
	}
	for name, in := range bad {
		if err := validateOrder(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: got %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestValidateDeliverySnapshot(t *testing.T) {
	src := func(s string) *string { return &s }
	valid := []DeliverySnapshotRequest{
		{},
		{Latitude: f64(17.4), Longitude: f64(78.5), GPSAccuracyM: f64(0), LocationSource: src("gps_confirmed")},
		{Latitude: f64(-90), Longitude: f64(180)},
	}
	for i, in := range valid {
		if err := validateDeliverySnapshot(in); err != nil {
			t.Errorf("valid %d refused: %v", i, err)
		}
	}
	invalid := map[string]DeliverySnapshotRequest{
		"lat without lon": {Latitude: f64(1)},
		"lon without lat": {Longitude: f64(1)},
		"lat range":       {Latitude: f64(90.1), Longitude: f64(0)},
		"lon range":       {Latitude: f64(0), Longitude: f64(-180.1)},
		"accuracy":        {GPSAccuracyM: f64(-1)},
		"source":          {LocationSource: src("guess")},
	}
	for name, in := range invalid {
		if err := validateDeliverySnapshot(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestHasUsableDeliverySnapshot(t *testing.T) {
	blank, line := "  ", "12 MG Road"
	cases := map[string]struct {
		in   *DeliverySnapshot
		want bool
	}{
		"nil":        {nil, false},
		"no line 1":  {&DeliverySnapshot{}, false},
		"blank":      {&DeliverySnapshot{AddressLine1: &blank}, false},
		"has line 1": {&DeliverySnapshot{AddressLine1: &line}, true},
	}
	for name, c := range cases {
		if got := hasUsableDeliverySnapshot(c.in); got != c.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func TestValidateItem(t *testing.T) {
	if err := validateItem(OrderItemRequest{PlantID: 1, Quantity: 3, UnitPrice: 10.5, TotalPrice: 31.5}); err != nil {
		t.Errorf("valid item refused: %v", err)
	}
	if err := validateItem(OrderItemRequest{PlantID: 1, Quantity: 3, UnitPrice: 10, TotalPrice: 30.009}); err != nil {
		t.Errorf("a paisa of rounding is allowed: %v", err)
	}
	for name, in := range map[string]OrderItemRequest{
		"no plant":       {Quantity: 1},
		"zero quantity":  {PlantID: 1},
		"negative price": {PlantID: 1, Quantity: 1, UnitPrice: -1, TotalPrice: -1},
		"total mismatch": {PlantID: 1, Quantity: 2, UnitPrice: 10, TotalPrice: 21},
	} {
		if err := validateItem(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestTotalPages(t *testing.T) {
	for _, c := range []struct {
		total   int64
		perPage int
		want    int
	}{{0, 20, 0}, {1, 20, 1}, {20, 20, 1}, {21, 20, 2}} {
		if got := totalPages(c.total, c.perPage); got != c.want {
			t.Errorf("totalPages(%d, %d) = %d, want %d", c.total, c.perPage, got, c.want)
		}
	}
}
