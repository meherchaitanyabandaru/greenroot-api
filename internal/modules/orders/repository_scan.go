// SQL fragments, row scanners and NULL helpers for the orders repository.

package orders

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func baseSelect() string {
	return `
		SELECT o.order_id, o.order_code, o.order_number,
			o.buyer_user_id, bu.first_name,
			o.seller_nursery_id, n.nursery_name,
			o.status::text, COALESCE(o.total_amount, 0), o.notes, o.ordered_at,
			o.created_at, o.updated_at,
			o.created_by_user_id, o.updated_by_user_id,
			o.quotation_id, o.customer_user_id,
			-- customer_* are never written today; fall back to the buyer's account
			COALESCE(o.customer_name, NULLIF(TRIM(CONCAT_WS(' ', bu.first_name, bu.last_name)), '')),
			COALESCE(o.customer_mobile, bu.mobile),
			o.assigned_manager_user_id, o.created_by_user_id,
			o.cancelled_by_user_id, o.cancelled_at, o.cancel_reason,
			o.loading_started_at, o.loading_completed_at, o.loading_completed_by_user_id,
			o.buyer_nursery_id, o.version, o.confirmed_at,
			mu.first_name
		FROM public.orders o
		LEFT JOIN public.users bu ON bu.user_id = o.buyer_user_id
		LEFT JOIN public.nurseries n ON n.nursery_id = o.seller_nursery_id
		LEFT JOIN public.users mu ON mu.user_id = o.assigned_manager_user_id
	`
}

func baseCount() string {
	return `
		SELECT COUNT(*)
		FROM public.orders o
		LEFT JOIN public.users bu ON bu.user_id = o.buyer_user_id
		LEFT JOIN public.nurseries n ON n.nursery_id = o.seller_nursery_id
	`
}

func itemSelect() string {
	return `
		SELECT oi.order_item_id, oi.order_id, COALESCE(oi.plant_id, 0), COALESCE(p.scientific_name, ''), p.common_name,
			oi.size_id, ps.size_code, ps.display_name, oi.quantity, oi.unit_price, oi.total_price,
			oi.remarks, oi.created_at, oi.loaded_quantity,
			CASE WHEN oi.plant_id IS NULL THEN oi.plant_name_snapshot END, oi.size
		FROM public.order_items oi
		LEFT JOIN public.plants p ON p.plant_id = oi.plant_id
		LEFT JOIN public.plant_sizes ps ON ps.size_id = oi.size_id
	`
}

func buildWhere(input ListOrdersRequest) (string, []any) {
	clauses := []string{"1 = 1"}
	args := make([]any, 0)

	if input.Buying {
		// Buyer perspective: orders where this user or their nursery is buyer
		buyerClauses := make([]string, 0)
		if input.BuyerID > 0 {
			args = append(args, input.BuyerID)
			buyerClauses = append(buyerClauses, fmt.Sprintf("o.buyer_user_id = $%d", len(args)))
		}
		if input.NurseryID > 0 {
			args = append(args, input.NurseryID)
			buyerClauses = append(buyerClauses, fmt.Sprintf("o.buyer_nursery_id = $%d", len(args)))
		}
		if len(buyerClauses) > 0 {
			clauses = append(clauses, "("+strings.Join(buyerClauses, " OR ")+")")
		}
	} else {
		// Seller perspective (default)
		if input.BuyerID > 0 {
			args = append(args, input.BuyerID)
			clauses = append(clauses, fmt.Sprintf("o.buyer_user_id = $%d", len(args)))
		}
		if input.NurseryID > 0 {
			args = append(args, input.NurseryID)
			clauses = append(clauses, fmt.Sprintf("o.seller_nursery_id = $%d", len(args)))
		}
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("o.status::text = ANY(string_to_array($%d::text, ','))", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		n := len(args)
		match := fmt.Sprintf("o.order_code ILIKE $%d OR o.order_number ILIKE $%d OR bu.first_name ILIKE $%d OR n.nursery_name ILIKE $%d", n, n, n, n)
		if input.SearchNotes {
			match += fmt.Sprintf(" OR o.notes ILIKE $%d", n)
		}
		clauses = append(clauses, "("+match+")")
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListOrdersRequest) string {
	direction := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		direction = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "id":
		return "o.order_id " + direction
	case "order_code":
		return "o.order_code " + direction + " NULLS LAST, o.order_id DESC"
	case "order_number":
		return "o.order_number " + direction + " NULLS LAST, o.order_id DESC"
	case "buyer_name":
		return "bu.first_name " + direction + " NULLS LAST, o.order_id DESC"
	case "seller_nursery":
		return "n.nursery_name " + direction + " NULLS LAST, o.order_id DESC"
	case "order_status", "status":
		return "o.status " + direction + " NULLS LAST, o.order_id DESC"
	case "total_amount":
		return "o.total_amount " + direction + " NULLS LAST, o.order_id DESC"
	case "order_date":
		return "o.ordered_at " + direction + " NULLS LAST, o.order_id DESC"
	case "created_at":
		return "o.created_at " + direction + " NULLS LAST, o.order_id DESC"
	default:
		return "o.order_id DESC"
	}
}

func scanOrderRow(row interface{ Scan(dest ...any) error }) (*Order, error) {
	order, err := scanOrder(row)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func scanOrderRows(rows *sql.Rows) (Order, error) {
	return scanOrder(rows)
}

func scanOrder(row interface{ Scan(dest ...any) error }) (Order, error) {
	var order Order
	var buyerID, sellerNurseryID, createdBy, updatedBy sql.NullInt64
	var buyerName, nurseryName, notes sql.NullString
	// V1 fields
	var quotationID, customerUserID sql.NullInt64
	var customerName, customerMobile sql.NullString
	var assignedManagerUserID, createdByUserID sql.NullInt64
	var cancelledByUserID sql.NullInt64
	var cancelledAt, loadingStartedAt, loadingCompletedAt, confirmedAt sql.NullTime
	var cancelReason sql.NullString
	var loadingCompletedByUserID sql.NullInt64
	var buyerNurseryID sql.NullInt64
	var assignedManagerName sql.NullString
	if err := row.Scan(
		&order.ID, &order.OrderCode, &order.OrderNumber,
		&buyerID, &buyerName,
		&sellerNurseryID, &nurseryName,
		&order.Status, &order.TotalAmount, &notes, &order.OrderDate,
		&order.CreatedAt, &order.UpdatedAt,
		&createdBy, &updatedBy,
		&quotationID, &customerUserID, &customerName, &customerMobile,
		&assignedManagerUserID, &createdByUserID,
		&cancelledByUserID, &cancelledAt, &cancelReason,
		&loadingStartedAt, &loadingCompletedAt, &loadingCompletedByUserID,
		&buyerNurseryID, &order.Version, &confirmedAt,
		&assignedManagerName,
	); err != nil {
		return Order{}, err
	}
	order.BuyerNurseryID = nullableInt64(buyerNurseryID)
	order.BuyerUserID = nullableInt64(buyerID)
	order.BuyerName = nullableString(buyerName)
	order.SellerNurseryID = nullableInt64(sellerNurseryID)
	order.SellerNursery = nullableString(nurseryName)
	order.NurseryName = nullableString(nurseryName)
	order.Notes = nullableString(notes)
	order.QuotationID = nullableInt64(quotationID)
	order.CustomerUserID = nullableInt64(customerUserID)
	order.CustomerName = nullableString(customerName)
	order.CustomerMobile = nullableString(customerMobile)
	order.AssignedManagerUserID = nullableInt64(assignedManagerUserID)
	order.AssignedManagerName = nullableString(assignedManagerName)
	order.CreatedByUserID = nullableInt64(createdByUserID)
	order.CancelledByUserID = nullableInt64(cancelledByUserID)
	if cancelledAt.Valid {
		order.CancelledAt = &cancelledAt.Time
	}
	order.CancelReason = nullableString(cancelReason)
	if confirmedAt.Valid {
		order.ConfirmedAt = &confirmedAt.Time
	}
	if loadingStartedAt.Valid {
		order.LoadingStartedAt = &loadingStartedAt.Time
	}
	if loadingCompletedAt.Valid {
		order.LoadingCompletedAt = &loadingCompletedAt.Time
	}
	order.LoadingCompletedByUserID = nullableInt64(loadingCompletedByUserID)
	return order, nil
}

func scanDeliverySnapshot(row interface{ Scan(dest ...any) error }) (DeliverySnapshot, error) {
	var snapshot DeliverySnapshot
	var contactName, contactMobile, alternateMobile sql.NullString
	var line1, line2, city, state, country, postal, landmark, instructions sql.NullString
	var latitude, longitude, accuracy sql.NullFloat64
	var source sql.NullString
	var confirmedBy, ackBy sql.NullInt64
	var confirmedAt, ackAt sql.NullTime
	if err := row.Scan(
		&snapshot.ID,
		&snapshot.OrderID,
		&contactName,
		&contactMobile,
		&alternateMobile,
		&line1,
		&line2,
		&city,
		&state,
		&country,
		&postal,
		&landmark,
		&instructions,
		&latitude,
		&longitude,
		&accuracy,
		&source,
		&confirmedBy,
		&confirmedAt,
		&snapshot.EmergencyUpdated,
		&snapshot.RequiresDriverAck,
		&ackBy,
		&ackAt,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
	); err != nil {
		return DeliverySnapshot{}, err
	}
	snapshot.ContactName = nullableString(contactName)
	snapshot.ContactMobile = nullableString(contactMobile)
	snapshot.AlternateMobile = nullableString(alternateMobile)
	snapshot.AddressLine1 = nullableString(line1)
	snapshot.AddressLine2 = nullableString(line2)
	snapshot.City = nullableString(city)
	snapshot.State = nullableString(state)
	snapshot.Country = nullableString(country)
	snapshot.PostalCode = nullableString(postal)
	snapshot.Landmark = nullableString(landmark)
	snapshot.DeliveryInstructions = nullableString(instructions)
	snapshot.Latitude = nullableFloat64(latitude)
	snapshot.Longitude = nullableFloat64(longitude)
	snapshot.GPSAccuracyM = nullableFloat64(accuracy)
	snapshot.LocationSource = nullableString(source)
	snapshot.ConfirmedBy = nullableInt64(confirmedBy)
	if confirmedAt.Valid {
		snapshot.ConfirmedAt = &confirmedAt.Time
	}
	snapshot.DriverAcknowledgedBy = nullableInt64(ackBy)
	if ackAt.Valid {
		snapshot.DriverAcknowledgedAt = &ackAt.Time
	}
	return snapshot, nil
}

func scanItemRow(row interface{ Scan(dest ...any) error }) (*OrderItem, error) {
	item, err := scanItem(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanItemRows(rows *sql.Rows) (OrderItem, error) {
	return scanItem(rows)
}

func scanItem(row interface{ Scan(dest ...any) error }) (OrderItem, error) {
	var item OrderItem
	var commonName, sizeCode, sizeName, remarks sql.NullString
	var sizeID sql.NullInt16
	var loadedQty sql.NullFloat64
	if err := row.Scan(&item.ID, &item.OrderID, &item.PlantID, &item.ScientificName, &commonName, &sizeID, &sizeCode, &sizeName, &item.Quantity, &item.UnitPrice, &item.TotalPrice, &remarks, &item.CreatedAt, &loadedQty,
		&item.CustomPlantName, &item.SizeDescription); err != nil {
		return OrderItem{}, err
	}
	item.IsCustom = item.PlantID == 0
	item.CommonName = nullableString(commonName)
	if sizeID.Valid {
		item.SizeID = &sizeID.Int16
	}
	item.SizeCode = nullableString(sizeCode)
	item.SizeName = nullableString(sizeName)
	item.Remarks = nullableString(remarks)
	if loadedQty.Valid {
		item.LoadedQuantity = &loadedQty.Float64
	}
	return item, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64OrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func int16OrNil(value *int16) any {
	if value == nil {
		return nil
	}
	return *value
}

func floatOrNil(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func timeOrNil(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func statusOrPending(value string) string {
	status := strings.ToUpper(strings.TrimSpace(value))
	if status == "" {
		return "PENDING"
	}
	return status
}

func scanOrderVerification(row interface{ Scan(dest ...any) error }) (*OrderVerification, error) {
	var v OrderVerification
	var revokedAt sql.NullTime
	var revokedBy sql.NullInt64
	err := row.Scan(&v.VerificationID, &v.OrderID, &v.Token, &v.Status, &v.CreatedAt, &revokedAt, &revokedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		v.RevokedAt = &revokedAt.Time
	}
	if revokedBy.Valid {
		v.RevokedBy = &revokedBy.Int64
	}
	return &v, nil
}

func joinAddressParts(line1, city, state, postal string) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{line1, city, state} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, strings.TrimSpace(p))
		}
	}
	addr := strings.Join(parts, ", ")
	if strings.TrimSpace(postal) != "" {
		if addr != "" {
			addr += " " + strings.TrimSpace(postal)
		} else {
			addr = strings.TrimSpace(postal)
		}
	}
	return addr
}
