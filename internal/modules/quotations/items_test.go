package quotations

import (
	"errors"
	"testing"
)

func sp(s string) *string { return &s }

func TestNormalizeItem(t *testing.T) {
	cases := []struct {
		name   string
		item   QuotationItemRequest
		priced bool
		ok     bool
	}{
		{"catalog plant, priced", QuotationItemRequest{PlantID: 4, Quantity: 10, UnitPrice: 5, TotalPrice: 50}, true, true},
		{"custom plant as the customer knows it", QuotationItemRequest{CustomPlantName: sp("  Nela   vemu "), Quantity: 100}, false, true},
		{"custom plant with variety, size and grade", QuotationItemRequest{CustomPlantName: sp("Banginapalli"), Variety: sp("grafted"), SizeDescription: sp("2 ft"), Grade: sp("A"), Quantity: 5}, false, true},
		{"neither a plant nor a name", QuotationItemRequest{Quantity: 1}, false, false},
		{"both a plant and a custom name", QuotationItemRequest{PlantID: 4, CustomPlantName: sp("Neem"), Quantity: 1}, false, false},
		{"a one-letter name", QuotationItemRequest{CustomPlantName: sp("x"), Quantity: 1}, false, false},
		{"no quantity", QuotationItemRequest{CustomPlantName: sp("Neem"), Quantity: 0}, false, false},
		{"a very long size", QuotationItemRequest{CustomPlantName: sp("Neem"), SizeDescription: sp(string(make([]byte, 200))), Quantity: 1}, false, false},
		{"priced lines must add up", QuotationItemRequest{PlantID: 4, Quantity: 10, UnitPrice: 5, TotalPrice: 49}, true, false},
		{"unpriced request lines carry no price check", QuotationItemRequest{PlantID: 4, Quantity: 10, UnitPrice: 0, TotalPrice: 0}, false, true},
	}
	for _, c := range cases {
		item := c.item
		err := normalizeItem(&item, c.priced)
		if c.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidInput)) {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
	item := QuotationItemRequest{CustomPlantName: sp("  Nela   vemu "), ScientificName: sp(" "), Quantity: 1}
	_ = normalizeItem(&item, false)
	if *item.CustomPlantName != "Nela vemu" || item.ScientificName != nil {
		t.Errorf("names are tidied, empty ones dropped: %+v", item)
	}
}

func TestItemNameKeepsTheCustomerName(t *testing.T) {
	common := "Mango"
	if got := itemName(QuotationItem{ScientificName: "Mangifera indica", CommonName: &common}); got != "Mango" {
		t.Errorf("catalog line: %q", got)
	}
	if got := itemName(QuotationItem{IsCustom: true, CustomPlantName: sp("Maamidi"), LinkedPlantName: &common}); got != "Maamidi" {
		t.Errorf("a custom line keeps the entered name even after GreenRoot links it: %q", got)
	}
}
