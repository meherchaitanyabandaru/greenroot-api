package quotations

import "time"

// policy.go — single source of truth for quotation business rules.
// All functions are pure, stateless, and take no database parameters.

// IsEditable returns true when a quotation's content may be modified.
func IsEditable(status string) bool {
	return status == "INTERNAL_DRAFT" || status == "CUSTOMER_DRAFT"
}

// IsDeletable: drafts, rejected, and sent-but-expired quotations. Accepted and
// converted ones are business records and stay.
func IsDeletable(q Quotation) bool {
	switch q.Status {
	case "INTERNAL_DRAFT", "CUSTOMER_DRAFT", "CUSTOMER_REJECTED":
		return q.ConvertedOrderID == nil
	case "CUSTOMER_SENT":
		return isExpiredTime(q.ValidUntil)
	}
	return false
}

// IsTerminal returns true when the quotation has reached a final state.
func IsTerminal(status string) bool {
	switch status {
	case "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED", "CANCELLED":
		return true
	}
	return false
}

// BuildCapabilities computes the capability set for the given actor and quotation.
// No database access — struct fields and actor roles are the only inputs.
func BuildCapabilities(actor ActorContext, q Quotation) *QuotationCapabilities {
	status := q.Status
	isAdmin := actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
	isSeller := isAdmin || actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER")
	isOwnerRole := isAdmin || actor.HasRole("NURSERY_OWNER")
	isCustomer := isAdmin || (q.CustomerUserID != nil && *q.CustomerUserID == actor.UserID)
	editable := IsEditable(status) && q.ConvertedOrderID == nil
	isCustomerSent := status == "CUSTOMER_SENT" && !isExpiredTime(q.ValidUntil)

	assignedToOther := q.AssignedManagerUserID != nil && *q.AssignedManagerUserID != actor.UserID && !isOwnerRole

	return &QuotationCapabilities{
		CanEdit:              isSeller && editable && !assignedToOther,
		CanUpdateCustomer:    isOwnerRole && q.QuotationType == "CUSTOMER" && q.ConvertedOrderID == nil && q.CustomerRespondedAt == nil,
		CanDelete:            isOwnerRole && IsDeletable(q),
		CanSend:              isSeller && status == "CUSTOMER_DRAFT",
		CanRecall:            isSeller && status == "CUSTOMER_SENT",
		CanAccept:            isCustomer && isCustomerSent,
		CanReject:            isCustomer && isCustomerSent,
		CanConvert:           isSeller && status == "CUSTOMER_ACCEPTED" && q.ConvertedOrderID == nil,
		CanAssignManager:     isOwnerRole && q.ConvertedOrderID == nil,
		CanGenerateDocument:  isSeller && q.DeletedAt == nil,
		CanManageVerifyToken: isSeller && q.DeletedAt == nil,
	}
}

func isExpiredTime(validUntil *time.Time) bool {
	return validUntil != nil && time.Now().After(*validUntil)
}
