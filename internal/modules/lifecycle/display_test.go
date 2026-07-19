package lifecycle

import "testing"

func TestOrderWithDispatchBuyerSeesDeliveryState(t *testing.T) {
	display := OrderWithDispatch("LOADED", "IN_TRANSIT")
	if display.Customer.Label != "On the Way" {
		t.Fatalf("customer label = %q, want On the Way", display.Customer.Label)
	}
	if display.Operator.Label != "Loaded" {
		t.Fatalf("operator label = %q, want Loaded", display.Operator.Label)
	}
}

func TestOrderWithDeliveredDispatchPromptsOperatorClose(t *testing.T) {
	display := OrderWithDispatch("LOADED", "DELIVERED")
	if display.Customer.Label != "Delivered" {
		t.Fatalf("customer label = %q, want Delivered", display.Customer.Label)
	}
	if display.Operator.Title != "Delivery Delivered" {
		t.Fatalf("operator title = %q, want Delivery Delivered", display.Operator.Title)
	}
}

func TestCompletedOrderCustomerLabelIsDelivered(t *testing.T) {
	display := Order("COMPLETED")
	if display.Customer.Label != "Delivered" {
		t.Fatalf("customer label = %q, want Delivered", display.Customer.Label)
	}
	if display.Operator.Label != "Completed" {
		t.Fatalf("operator label = %q, want Completed", display.Operator.Label)
	}
}

func TestDispatchRoleDisplays(t *testing.T) {
	display := Dispatch("IN_TRANSIT")
	if display.Customer.Label != "On the Way" {
		t.Fatalf("customer label = %q, want On the Way", display.Customer.Label)
	}
	if display.Driver.Label != "In Transit" {
		t.Fatalf("driver label = %q, want In Transit", display.Driver.Label)
	}
}
