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

type Actions struct {
	Customer []string `json:"customer,omitempty"`
	Operator []string `json:"operator,omitempty"`
	Driver   []string `json:"driver,omitempty"`
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
