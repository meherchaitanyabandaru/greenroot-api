package lifecycle

import "strings"

type Display struct {
	Label    string `json:"label"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Variant  string `json:"variant"`
}

type OrderDisplays struct {
	Customer    Display `json:"customer"`
	Operator    Display `json:"operator"`
	Driver      Display `json:"driver"`
	NextActions Actions `json:"next_actions"`
}

type DispatchDisplays struct {
	Customer    Display `json:"customer"`
	Operator    Display `json:"operator"`
	Driver      Display `json:"driver"`
	NextActions Actions `json:"next_actions"`
}

type QuotationDisplays struct {
	Customer    Display `json:"customer"`
	Operator    Display `json:"operator"`
	NextActions Actions `json:"next_actions"`
}

type PlantRequestDisplays struct {
	Requester   Display `json:"requester"`
	Supplier    Display `json:"supplier"`
	NextActions Actions `json:"next_actions"`
}

type SubscriptionDisplays struct {
	Customer    Display `json:"customer"`
	Operator    Display `json:"operator"`
	NextActions Actions `json:"next_actions"`
}

type Actions struct {
	Customer []string `json:"customer,omitempty"`
	Operator []string `json:"operator,omitempty"`
	Driver   []string `json:"driver,omitempty"`
	Supplier []string `json:"supplier,omitempty"`
}

func Order(status string) OrderDisplays {
	status = strings.ToUpper(strings.TrimSpace(status))
	return OrderDisplays{
		Customer:    orderDisplay(status, true),
		Operator:    orderDisplay(status, false),
		Driver:      orderDisplay(status, false),
		NextActions: orderActions(status, ""),
	}
}

func OrderWithDispatch(orderStatus string, dispatchStatus string) OrderDisplays {
	orderStatus = strings.ToUpper(strings.TrimSpace(orderStatus))
	dispatchStatus = strings.ToUpper(strings.TrimSpace(dispatchStatus))
	displays := Order(orderStatus)
	if orderStatus != "COMPLETED" && dispatchStatus != "" && dispatchStatus != "CANCELLED" {
		displays.Customer = buyerDispatchDisplay(dispatchStatus)
	}
	if orderStatus != "COMPLETED" && dispatchStatus == "DELIVERED" {
		displays.Operator = Display{
			Label:    "Delivered",
			Title:    "Delivery Delivered",
			Subtitle: "Review and close the order.",
			Variant:  "success",
		}
	}
	displays.NextActions = orderActions(orderStatus, dispatchStatus)
	return displays
}

func Dispatch(status string) DispatchDisplays {
	status = strings.ToUpper(strings.TrimSpace(status))
	driver := dispatchDisplay(status)
	return DispatchDisplays{
		Customer:    buyerDispatchDisplay(status),
		Operator:    driver,
		Driver:      driver,
		NextActions: dispatchActions(status),
	}
}

func Quotation(status string) QuotationDisplays {
	status = strings.ToUpper(strings.TrimSpace(status))
	return QuotationDisplays{
		Customer:    quotationCustomerDisplay(status),
		Operator:    quotationOperatorDisplay(status),
		NextActions: quotationActions(status),
	}
}

func PlantRequest(status string) PlantRequestDisplays {
	status = strings.ToUpper(strings.TrimSpace(status))
	return PlantRequestDisplays{
		Requester:   plantRequestDisplay(status, true),
		Supplier:    plantRequestDisplay(status, false),
		NextActions: plantRequestActions(status),
	}
}

func Subscription(status string, daysRemaining *int) SubscriptionDisplays {
	status = strings.ToUpper(strings.TrimSpace(status))
	return SubscriptionDisplays{
		Customer:    subscriptionDisplay(status, daysRemaining, true),
		Operator:    subscriptionDisplay(status, daysRemaining, false),
		NextActions: subscriptionActions(status, daysRemaining),
	}
}

func orderActions(orderStatus string, dispatchStatus string) Actions {
	switch orderStatus {
	case "PENDING":
		return Actions{Customer: []string{"View Order"}, Operator: []string{"Confirm Order", "Cancel Order"}}
	case "CONFIRMED":
		return Actions{Customer: []string{"View Order"}, Operator: []string{"Start Loading"}}
	case "LOADING":
		return Actions{Customer: []string{"View Order"}, Operator: []string{"Complete Loading", "Adjust Loaded Quantities"}}
	case "LOADED", "PARTIALLY_FULFILLED":
		switch dispatchStatus {
		case "DELIVERED":
			return Actions{Customer: []string{"View Delivery"}, Operator: []string{"Complete Order", "View Dispatch"}, Driver: []string{"View Completed Trip"}}
		case "IN_TRANSIT", "DISPATCHED":
			return Actions{Customer: []string{"Track Delivery"}, Operator: []string{"View Dispatch"}, Driver: []string{"Update Trip"}}
		case "ACCEPTED":
			return Actions{Customer: []string{"View Delivery"}, Operator: []string{"View Dispatch"}, Driver: []string{"Start Trip"}}
		case "PENDING":
			return Actions{Customer: []string{"View Order"}, Operator: []string{"Assign Driver", "View Dispatch"}, Driver: []string{"Accept Trip"}}
		default:
			return Actions{Customer: []string{"View Order"}, Operator: []string{"Create Dispatch"}}
		}
	case "COMPLETED":
		return Actions{Customer: []string{"View Receipt", "Rate Order"}, Operator: []string{"View Order"}}
	case "CANCELLED":
		return Actions{Customer: []string{"View Order"}, Operator: []string{"View Order"}}
	default:
		return Actions{Customer: []string{"View Order"}, Operator: []string{"View Order"}}
	}
}

func dispatchActions(status string) Actions {
	switch status {
	case "PENDING":
		return Actions{Customer: []string{"View Delivery"}, Operator: []string{"Assign Driver"}, Driver: []string{"Accept Trip"}}
	case "ACCEPTED":
		return Actions{Customer: []string{"View Delivery"}, Operator: []string{"View Dispatch"}, Driver: []string{"Start Trip"}}
	case "DISPATCHED":
		return Actions{Customer: []string{"Track Delivery"}, Operator: []string{"View Dispatch"}, Driver: []string{"Start Navigation"}}
	case "IN_TRANSIT":
		return Actions{Customer: []string{"Track Delivery"}, Operator: []string{"View Dispatch"}, Driver: []string{"Mark Delivered"}}
	case "DELIVERED":
		return Actions{Customer: []string{"View Delivery"}, Operator: []string{"Complete Order"}, Driver: []string{"View Completed Trip"}}
	case "CANCELLED":
		return Actions{Customer: []string{"View Delivery"}, Operator: []string{"View Dispatch"}, Driver: []string{"View Trip"}}
	default:
		return Actions{Customer: []string{"View Delivery"}, Operator: []string{"View Dispatch"}, Driver: []string{"View Trip"}}
	}
}

func quotationActions(status string) Actions {
	switch status {
	case "INTERNAL_DRAFT":
		return Actions{Operator: []string{"Edit Quotation", "Send Internally"}}
	case "CUSTOMER_DRAFT":
		return Actions{Operator: []string{"Edit Quotation", "Send to Customer"}}
	case "CUSTOMER_SENT":
		return Actions{Customer: []string{"Accept Quotation", "Reject Quotation"}, Operator: []string{"Recall Quotation", "View Customer Response"}}
	case "CUSTOMER_ACCEPTED":
		return Actions{Customer: []string{"View Accepted Quotation"}, Operator: []string{"Convert to Order"}}
	case "CUSTOMER_REJECTED":
		return Actions{Customer: []string{"View Quotation"}, Operator: []string{"Review Rejection"}}
	case "CONVERTED":
		return Actions{Customer: []string{"View Order"}, Operator: []string{"View Order"}}
	case "EXPIRED":
		return Actions{Customer: []string{"View Expired Quotation"}, Operator: []string{"Create New Quotation"}}
	default:
		return Actions{Customer: []string{"View Quotation"}, Operator: []string{"View Quotation"}}
	}
}

func plantRequestActions(status string) Actions {
	switch status {
	case "DRAFT":
		return Actions{Operator: []string{"Publish Request"}}
	case "OPEN":
		return Actions{Operator: []string{"Review Responses", "Close Request"}, Supplier: []string{"Respond to Request"}}
	case "PARTIALLY_ACCEPTED":
		return Actions{Operator: []string{"Accept More Responses", "Close Request"}, Supplier: []string{"Respond to Request"}}
	case "ACCEPTED":
		return Actions{Operator: []string{"View Accepted Responses"}, Supplier: []string{"View Request"}}
	case "REJECTED":
		return Actions{Operator: []string{"View Request"}, Supplier: []string{"View Request"}}
	case "CLOSED":
		return Actions{Operator: []string{"View Request"}, Supplier: []string{"View Request"}}
	default:
		return Actions{Operator: []string{"View Request"}, Supplier: []string{"View Request"}}
	}
}

func subscriptionActions(status string, daysRemaining *int) Actions {
	switch status {
	case "ACTIVE":
		if daysRemaining != nil && *daysRemaining <= 14 {
			return Actions{Customer: []string{"Renew Subscription"}, Operator: []string{"Review Renewal"}}
		}
		return Actions{Customer: []string{"Manage Subscription"}, Operator: []string{"View Subscription"}}
	case "PAUSED":
		return Actions{Customer: []string{"Resume Subscription"}, Operator: []string{"Resume Subscription", "Cancel Subscription"}}
	case "CANCELLED":
		return Actions{Customer: []string{"Renew Subscription"}, Operator: []string{"View Subscription"}}
	case "EXPIRED":
		return Actions{Customer: []string{"Renew Subscription"}, Operator: []string{"Renew Subscription"}}
	default:
		return Actions{Customer: []string{"View Subscription"}, Operator: []string{"View Subscription"}}
	}
}

func orderDisplay(status string, customer bool) Display {
	switch status {
	case "PENDING":
		if customer {
			return Display{"Pending", "Waiting for Confirmation", "The nursery will review and confirm your order.", "warning"}
		}
		return Display{"Pending", "New Order", "Confirm this order to begin preparation.", "warning"}
	case "CONFIRMED":
		if customer {
			return Display{"Confirmed", "Order Confirmed", "The nursery has confirmed your order.", "info"}
		}
		return Display{"Confirmed", "Confirmed - Ready to Load", "Start loading items to prepare for dispatch.", "info"}
	case "LOADING":
		return Display{"Loading", "Loading in Progress", "Items are being prepared.", "warning"}
	case "LOADED":
		return Display{"Loaded", "Order Loaded", "Ready for delivery.", "success"}
	case "PARTIALLY_FULFILLED":
		return Display{"Partially Fulfilled", "Partially Fulfilled", "Some items had reduced quantities.", "accent"}
	case "COMPLETED":
		if customer {
			return Display{"Delivered", "Delivered", "Order delivered and completed.", "success"}
		}
		return Display{"Completed", "Order Completed", "Order delivered and completed.", "success"}
	case "CANCELLED":
		return Display{"Cancelled", "Order Cancelled", "This order has been cancelled.", "error"}
	default:
		label := pretty(status)
		return Display{label, label, "", "neutral"}
	}
}

func buyerDispatchDisplay(status string) Display {
	switch status {
	case "PENDING":
		return Display{"Delivery Pending", "Delivery Being Arranged", "The nursery is arranging your delivery.", "neutral"}
	case "ACCEPTED":
		return Display{"Driver Assigned", "Driver Assigned", "A driver has accepted your delivery.", "info"}
	case "DISPATCHED":
		return Display{"Out for Delivery", "Out for Delivery", "Your order has left the nursery.", "info"}
	case "IN_TRANSIT":
		return Display{"On the Way", "On the Way", "Your delivery is on the way.", "warning"}
	case "DELIVERED":
		return Display{"Delivered", "Delivered", "Your order has been delivered.", "success"}
	case "CANCELLED":
		return Display{"Cancelled", "Dispatch Cancelled", "Delivery was cancelled.", "error"}
	default:
		return dispatchDisplay(status)
	}
}

func dispatchDisplay(status string) Display {
	switch status {
	case "PENDING":
		return Display{"Pending", "Dispatch Created", "Awaiting driver.", "warning"}
	case "ACCEPTED":
		return Display{"Accepted", "Driver Accepted", "Driver has accepted the trip.", "info"}
	case "DISPATCHED":
		return Display{"Dispatched", "Out for Delivery", "Order has left the nursery.", "info"}
	case "IN_TRANSIT":
		return Display{"In Transit", "In Transit", "Delivery is on the way.", "warning"}
	case "DELIVERED":
		return Display{"Delivered", "Delivered", "Delivery is complete.", "success"}
	case "CANCELLED":
		return Display{"Cancelled", "Dispatch Cancelled", "Delivery was cancelled.", "error"}
	default:
		label := pretty(status)
		return Display{label, label, "", "neutral"}
	}
}

func quotationCustomerDisplay(status string) Display {
	switch status {
	case "INTERNAL_DRAFT":
		return Display{"Draft", "Quotation Draft", "This quotation is not visible to the customer yet.", "neutral"}
	case "CUSTOMER_DRAFT":
		return Display{"Draft", "Quotation Being Prepared", "The nursery is preparing your quotation.", "neutral"}
	case "CUSTOMER_SENT":
		return Display{"Ready for Review", "Quotation Ready", "Review the quotation and respond.", "warning"}
	case "CUSTOMER_ACCEPTED":
		return Display{"Accepted", "Quotation Accepted", "The nursery can convert this quotation to an order.", "success"}
	case "CUSTOMER_REJECTED":
		return Display{"Rejected", "Quotation Rejected", "You rejected this quotation.", "error"}
	case "CONVERTED":
		return Display{"Converted", "Order Created", "This quotation has been converted to an order.", "success"}
	case "EXPIRED":
		return Display{"Expired", "Quotation Expired", "This quotation is no longer valid.", "error"}
	default:
		label := pretty(status)
		return Display{label, label, "", "neutral"}
	}
}

func quotationOperatorDisplay(status string) Display {
	switch status {
	case "INTERNAL_DRAFT":
		return Display{"Internal Draft", "Internal Quotation Draft", "Complete the quotation before customer sharing.", "neutral"}
	case "CUSTOMER_DRAFT":
		return Display{"Customer Draft", "Customer Quotation Draft", "Send this quotation to the customer when ready.", "neutral"}
	case "CUSTOMER_SENT":
		return Display{"Sent", "Awaiting Customer", "Customer response is pending.", "warning"}
	case "CUSTOMER_ACCEPTED":
		return Display{"Accepted", "Customer Accepted", "Convert this quotation to an order.", "success"}
	case "CUSTOMER_REJECTED":
		return Display{"Rejected", "Customer Rejected", "Review the rejection before next follow-up.", "error"}
	case "CONVERTED":
		return Display{"Converted", "Converted to Order", "This quotation is locked.", "success"}
	case "EXPIRED":
		return Display{"Expired", "Quotation Expired", "Create a fresh quotation if needed.", "error"}
	default:
		label := pretty(status)
		return Display{label, label, "", "neutral"}
	}
}

func plantRequestDisplay(status string, requester bool) Display {
	switch status {
	case "DRAFT":
		return Display{"Draft", "Request Draft", "Publish the request when ready.", "neutral"}
	case "OPEN":
		if requester {
			return Display{"Open", "Awaiting Supplier Responses", "Suppliers can respond to this request.", "warning"}
		}
		return Display{"Open", "Response Needed", "Submit availability for this request.", "warning"}
	case "PARTIALLY_ACCEPTED":
		return Display{"Partially Accepted", "Partially Accepted", "Some requested quantity has been accepted.", "accent"}
	case "ACCEPTED":
		return Display{"Accepted", "Request Accepted", "Required quantity has been accepted.", "success"}
	case "REJECTED":
		return Display{"Rejected", "Request Rejected", "This request was rejected.", "error"}
	case "CLOSED":
		return Display{"Closed", "Request Closed", "No more responses are needed.", "neutral"}
	default:
		label := pretty(status)
		return Display{label, label, "", "neutral"}
	}
}

func subscriptionDisplay(status string, daysRemaining *int, customer bool) Display {
	if status == "ACTIVE" && daysRemaining != nil {
		if *daysRemaining < 0 {
			return Display{"Expired", "Subscription Expired", "Renew to continue using paid features.", "error"}
		}
		if *daysRemaining <= 14 {
			return Display{"Expiring Soon", "Subscription Expiring Soon", "Renew before expiry to avoid interruption.", "warning"}
		}
	}
	switch status {
	case "ACTIVE":
		return Display{"Active", "Subscription Active", "Your subscription is active.", "success"}
	case "PAUSED":
		return Display{"Paused", "Subscription Paused", "Resume when ready.", "warning"}
	case "CANCELLED":
		return Display{"Cancelled", "Subscription Cancelled", "Renew to activate again.", "error"}
	case "EXPIRED":
		return Display{"Expired", "Subscription Expired", "Renew to continue using paid features.", "error"}
	default:
		label := pretty(status)
		return Display{label, label, "", "neutral"}
	}
}

func pretty(status string) string {
	parts := strings.Split(strings.ToLower(status), "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
