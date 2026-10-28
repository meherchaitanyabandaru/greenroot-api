package lifecycle

import "testing"

func TestOrderWithDispatchBuyerSeesDeliveryState(t *testing.T) {
	display := OrderWithDispatch("LOADED", "IN_TRANSIT")
	if display.Customer.Label != "On the Way" {
		t.Fatalf("customer label = %q, want On the Way", display.Customer.Label)
	}
	// The owner's list follows the truck too, not "Loaded".
	if display.Operator.Label != "On the Way" || display.Driver.Label != "On the Way" {
		t.Fatalf("operator/driver label = %q/%q, want On the Way", display.Operator.Label, display.Driver.Label)
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
	if display.Driver.Label != "On the Way" {
		t.Fatalf("driver label = %q, want On the Way", display.Driver.Label)
	}
	// Dispatched and in transit read the same to everyone.
	if d := Dispatch("DISPATCHED"); d.Customer.Label != "On the Way" || d.Operator.Label != "On the Way" {
		t.Fatalf("dispatched labels = %q/%q, want On the Way", d.Customer.Label, d.Operator.Label)
	}
}
