package quotations

import "testing"

func TestQuotationIsEditable(t *testing.T) {
	editable := []string{"INTERNAL_DRAFT", "CUSTOMER_DRAFT"}
	locked := []string{"CUSTOMER_SENT", "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED", "CANCELLED"}
	for _, s := range editable {
		if !IsEditable(s) {
			t.Errorf("IsEditable(%s): want true", s)
		}
	}
	for _, s := range locked {
		if IsEditable(s) {
			t.Errorf("IsEditable(%s): want false", s)
		}
	}
}

func TestQuotationIsTerminal(t *testing.T) {
	terminal := []string{"CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED", "CANCELLED"}
	active := []string{"INTERNAL_DRAFT", "CUSTOMER_DRAFT", "CUSTOMER_SENT"}
	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%s): want true", s)
		}
	}
	for _, s := range active {
		if IsTerminal(s) {
			t.Errorf("IsTerminal(%s): want false", s)
		}
	}
}

func TestQuotationBuildCapabilities(t *testing.T) {
	owner := ActorContext{UserID: 1, Roles: []string{"NURSERY_OWNER"}}
	mgr := ActorContext{UserID: 2, Roles: []string{"MANAGER"}}
	customer := ActorContext{UserID: 5, Roles: []string{"BUYER"}}

	customerID := int64(5)

	t.Run("owner_internal_draft", func(t *testing.T) {
		q := Quotation{Status: "INTERNAL_DRAFT"}
		caps := BuildCapabilities(owner, q)
		if !caps.CanEdit || !caps.CanDelete || !caps.CanGenerateDocument {
			t.Error("owner at INTERNAL_DRAFT should have edit/delete/generate caps")
		}
		if caps.CanSend {
			t.Error("owner should not CanSend at INTERNAL_DRAFT")
		}
	})

	t.Run("owner_customer_draft", func(t *testing.T) {
		q := Quotation{Status: "CUSTOMER_DRAFT"}
		caps := BuildCapabilities(owner, q)
		if !caps.CanSend {
			t.Error("owner should CanSend at CUSTOMER_DRAFT")
		}
	})

	t.Run("owner_customer_sent", func(t *testing.T) {
		q := Quotation{Status: "CUSTOMER_SENT"}
		caps := BuildCapabilities(owner, q)
		if !caps.CanRecall {
			t.Error("owner should CanRecall at CUSTOMER_SENT")
		}
		if caps.CanEdit {
			t.Error("owner should not CanEdit at CUSTOMER_SENT")
		}
	})

	t.Run("customer_can_accept_sent", func(t *testing.T) {
		q := Quotation{Status: "CUSTOMER_SENT", CustomerUserID: &customerID}
		caps := BuildCapabilities(customer, q)
		if !caps.CanAccept || !caps.CanReject {
			t.Error("customer should CanAccept/CanReject at CUSTOMER_SENT")
		}
	})

	t.Run("manager_cannot_delete", func(t *testing.T) {
		q := Quotation{Status: "INTERNAL_DRAFT"}
		caps := BuildCapabilities(mgr, q)
		if caps.CanDelete {
			t.Error("manager should not CanDelete (owner-only)")
		}
		if !caps.CanEdit {
			t.Error("manager should CanEdit at INTERNAL_DRAFT")
		}
	})

	t.Run("converted_blocks_actions", func(t *testing.T) {
		orderID := int64(99)
		q := Quotation{Status: "INTERNAL_DRAFT", ConvertedOrderID: &orderID}
		caps := BuildCapabilities(owner, q)
		if caps.CanEdit || caps.CanDelete || caps.CanConvert {
			t.Error("converted quotation should block edit/delete/convert")
		}
	})
}
