// SQL fragments, row scanners and NULL helpers for the quotations repository.

package quotations

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

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

func baseSelect() string {
	return `
		SELECT q.quotation_id, q.quotation_code, q.quotation_type,
		       q.created_by_user_id, q.created_by_name,
		       q.nursery_id, q.nursery_name, q.nursery_phone,
		       q.customer_user_id, q.assigned_manager_user_id, q.converted_order_id,
		       q.recipient_name, q.recipient_mobile, q.notes, q.rejection_reason,
		       COALESCE(q.total_amount, 0), q.status, q.valid_until,
		       q.sent_at, q.customer_responded_at,
		       q.deleted_at, q.created_at, q.updated_at, q.version,
		       q.buyer_nursery_id,
		       NULLIF(TRIM(COALESCE(um.first_name, '') || ' ' || COALESCE(um.last_name, '')), '') AS assigned_manager_name,
		       o.order_code AS converted_order_code,
		       q.converted_at,
		       n.brand_color AS nursery_brand_color,
		       na.latitude::double precision, na.longitude::double precision,
		       q.requested_by_user_id, q.request_note, q.request_expires_at,
		       q.decline_reason, q.declined_at, q.cancelled_at
		FROM public.quotations q
		LEFT JOIN public.users um ON um.user_id = q.assigned_manager_user_id
		LEFT JOIN public.orders o ON o.order_id = q.converted_order_id
		LEFT JOIN public.nurseries n ON n.nursery_id = q.nursery_id
		-- Where the plants leave from: the buyer sees roughly how far that is.
		LEFT JOIN LATERAL (
			SELECT a.latitude, a.longitude FROM public.nursery_addresses a
			WHERE a.nursery_id = q.nursery_id
			ORDER BY a.is_primary DESC, a.nursery_address_id LIMIT 1
		) na ON true
		WHERE q.deleted_at IS NULL
	`
}

func baseCount() string {
	return `SELECT COUNT(*) FROM public.quotations q WHERE q.deleted_at IS NULL`
}

func itemSelect() string {
	return `
		SELECT qi.quotation_item_id, qi.quotation_id, qi.plant_id,
		       COALESCE(qi.scientific_name, ''), qi.common_name, qi.description,
		       qi.quantity, qi.unit_price, qi.total_price, qi.created_at,
		       qi.custom_plant_name, qi.plant_suggestion_id,
		       CASE WHEN ps.status IN ('LINKED', 'APPROVED') THEN COALESCE(NULLIF(lp.common_name, ''), lp.scientific_name) END,
		       qi.variety, qi.size_description, qi.grade
		FROM public.quotation_items qi
		LEFT JOIN public.plant_suggestions ps ON ps.suggestion_id = qi.plant_suggestion_id
		LEFT JOIN public.plants lp ON lp.plant_id = ps.plant_id
	`
}

func buildWhere(input ListQuotationsRequest) (string, []any) {
	clauses := make([]string, 0)
	args := make([]any, 0)

	if input.Buying {
		// Buyer perspective: quotations where this user (or their nursery) is the buyer/recipient.
		// Matches both linked accounts (customer_user_id) and unlinked mobile-only quotations.
		clauses = append(clauses, "q.status IN ('CUSTOMER_SENT', 'CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED', 'CUSTOMER_REQUESTED', 'REQUEST_DECLINED', 'REQUEST_CANCELLED')")
		buyerClauses := make([]string, 0)
		if input.UserID > 0 {
			args = append(args, input.UserID)
			buyerClauses = append(buyerClauses, fmt.Sprintf("q.customer_user_id = $%d", len(args)))
			// Also match quotations sent to this user's mobile number (not yet linked).
			buyerClauses = append(buyerClauses, fmt.Sprintf(
				"q.recipient_mobile = (SELECT mobile FROM public.users WHERE user_id = $%d)", len(args),
			))
		}
		if input.BuyerNurseryID > 0 {
			args = append(args, input.BuyerNurseryID)
			buyerClauses = append(buyerClauses, fmt.Sprintf("q.buyer_nursery_id = $%d", len(args)))
		}
		if len(buyerClauses) > 0 {
			clauses = append(clauses, "("+strings.Join(buyerClauses, " OR ")+")")
		}
	} else {
		// Seller perspective (default)
		if input.UserID > 0 {
			args = append(args, input.UserID)
			clauses = append(clauses, fmt.Sprintf("q.created_by_user_id = $%d", len(args)))
		}
		if input.NurseryID > 0 {
			args = append(args, input.NurseryID)
			clauses = append(clauses, fmt.Sprintf("q.nursery_id = $%d", len(args)))
		}
		// Manager scope: restrict to quotations this manager created or is assigned to.
		if input.ManagerScopeUserID > 0 {
			args = append(args, input.ManagerScopeUserID)
			n := len(args)
			clauses = append(clauses, fmt.Sprintf(
				"(q.created_by_user_id = $%d OR q.assigned_manager_user_id = $%d)", n, n,
			))
		}
		// Owner unassigned filter: only quotations with no assigned manager.
		if input.UnassignedOnly {
			clauses = append(clauses, "q.assigned_manager_user_id IS NULL")
		}
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("q.status::text = ANY(string_to_array($%d::text, ','))", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(q.quotation_code ILIKE $%d OR q.created_by_name ILIKE $%d OR q.recipient_name ILIKE $%d OR q.nursery_name ILIKE $%d)", len(args), len(args), len(args), len(args)))
	}
	if input.DateFrom != nil {
		args = append(args, *input.DateFrom)
		clauses = append(clauses, fmt.Sprintf("q.created_at >= $%d", len(args)))
	}
	if input.DateTo != nil {
		// Include the full end day by going to start of next day.
		endOfDay := input.DateTo.AddDate(0, 0, 1)
		args = append(args, endOfDay)
		clauses = append(clauses, fmt.Sprintf("q.created_at < $%d", len(args)))
	}
	if input.AmountMin != nil {
		args = append(args, *input.AmountMin)
		clauses = append(clauses, fmt.Sprintf("q.total_amount >= $%d", len(args)))
	}
	if input.AmountMax != nil {
		args = append(args, *input.AmountMax)
		clauses = append(clauses, fmt.Sprintf("q.total_amount <= $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "AND " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListQuotationsRequest) string {
	dir := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		dir = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "quotation_code":
		return "q.quotation_code " + dir
	case "total_amount":
		return "q.total_amount " + dir
	case "status":
		return "q.status " + dir
	case "created_at":
		return "q.created_at " + dir + ", q.quotation_id DESC"
	default:
		return "q.quotation_id DESC"
	}
}

func scanQuotationRow(row interface{ Scan(dest ...any) error }) (*Quotation, error) {
	q, err := scanQuotation(row)
	if err != nil {
		return nil, err
	}
	return &q, nil
}

func scanQuotationRows(rows *sql.Rows) (Quotation, error) {
	return scanQuotation(rows)
}

func scanQuotation(row interface{ Scan(dest ...any) error }) (Quotation, error) {
	var q Quotation
	var (
		createdByName          sql.NullString
		nurseryID              sql.NullInt64
		nurseryName            sql.NullString
		nurseryPhone           sql.NullString
		customerUserID         sql.NullInt64
		assignedManagerUserID  sql.NullInt64
		convertedOrderID       sql.NullInt64
		recipientName          sql.NullString
		recipientMobile        sql.NullString
		notes                  sql.NullString
		rejectionReason        sql.NullString
		validUntil             sql.NullTime
		sentAt                 sql.NullTime
		customerRespondedAt    sql.NullTime
		deletedAt              sql.NullTime
		buyerNurseryID         sql.NullInt64
		totalAmount            sql.NullString
		assignedManagerName    sql.NullString
		convertedOrderCode     sql.NullString
		convertedAt            sql.NullTime
		nurseryBrandColor      sql.NullString
		nurseryLat, nurseryLon sql.NullFloat64
	)
	if err := row.Scan(
		&q.ID, &q.QuotationCode, &q.QuotationType,
		&q.CreatedByUserID, &createdByName,
		&nurseryID, &nurseryName, &nurseryPhone,
		&customerUserID, &assignedManagerUserID, &convertedOrderID,
		&recipientName, &recipientMobile, &notes, &rejectionReason,
		&totalAmount, &q.Status, &validUntil,
		&sentAt, &customerRespondedAt,
		&deletedAt, &q.CreatedAt, &q.UpdatedAt, &q.Version,
		&buyerNurseryID, &assignedManagerName,
		&convertedOrderCode, &convertedAt,
		&nurseryBrandColor,
		&nurseryLat, &nurseryLon,
		&q.RequestedByUserID, &q.RequestNote, &q.RequestExpiresAt,
		&q.DeclineReason, &q.DeclinedAt, &q.CancelledAt,
	); err != nil {
		return Quotation{}, err
	}
	if totalAmount.Valid && totalAmount.String != "" {
		q.TotalAmount, _ = strconv.ParseFloat(totalAmount.String, 64)
	}
	q.IsRequest = q.RequestedByUserID != nil
	q.CreatedByName = nullableString(createdByName)
	q.NurseryID = nullableInt64(nurseryID)
	q.NurseryName = nullableString(nurseryName)
	q.NurseryPhone = nullableString(nurseryPhone)
	q.NurseryBrandColor = nullableString(nurseryBrandColor)
	if nurseryLat.Valid && nurseryLon.Valid {
		q.NurseryLatitude, q.NurseryLongitude = &nurseryLat.Float64, &nurseryLon.Float64
	}
	q.CustomerUserID = nullableInt64(customerUserID)
	q.BuyerNurseryID = nullableInt64(buyerNurseryID)
	q.AssignedManagerUserID = nullableInt64(assignedManagerUserID)
	q.AssignedManagerName = nullableString(assignedManagerName)
	q.ConvertedOrderID = nullableInt64(convertedOrderID)
	q.ConvertedOrderCode = nullableString(convertedOrderCode)
	if convertedAt.Valid {
		q.ConvertedAt = &convertedAt.Time
	}
	q.RecipientName = nullableString(recipientName)
	q.RecipientMobile = nullableString(recipientMobile)
	q.Notes = nullableString(notes)
	q.RejectionReason = nullableString(rejectionReason)
	if validUntil.Valid {
		q.ValidUntil = &validUntil.Time
	}
	if sentAt.Valid {
		q.SentAt = &sentAt.Time
	}
	if customerRespondedAt.Valid {
		q.CustomerRespondedAt = &customerRespondedAt.Time
	}
	if deletedAt.Valid {
		q.DeletedAt = &deletedAt.Time
	}
	return q, nil
}

func scanItemRows(rows *sql.Rows) (QuotationItem, error) {
	return scanItem(rows)
}

func scanItem(row interface{ Scan(dest ...any) error }) (QuotationItem, error) {
	var item QuotationItem
	var commonName, description sql.NullString
	var quantity, unitPrice, totalPrice sql.NullString
	if err := row.Scan(
		&item.ID, &item.QuotationID, &item.PlantID,
		&item.ScientificName, &commonName, &description,
		&quantity, &unitPrice, &totalPrice, &item.CreatedAt,
		&item.CustomPlantName, &item.PlantSuggestionID, &item.LinkedPlantName,
		&item.Variety, &item.SizeDescription, &item.Grade,
	); err != nil {
		return QuotationItem{}, err
	}
	item.IsCustom = item.PlantID == nil
	if quantity.Valid && quantity.String != "" {
		item.Quantity, _ = strconv.ParseFloat(quantity.String, 64)
	}
	if unitPrice.Valid && unitPrice.String != "" {
		item.UnitPrice, _ = strconv.ParseFloat(unitPrice.String, 64)
		item.IsPriced = true
	}
	if totalPrice.Valid && totalPrice.String != "" {
		item.TotalPrice, _ = strconv.ParseFloat(totalPrice.String, 64)
	}
	item.CommonName = nullableString(commonName)
	item.Description = nullableString(description)
	return item, nil
}

func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullableInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func stringOrNil(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	return *v
}

func int64OrNil(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// Item field limits.
const (
	maxCustomName = 255
	maxSpecLength = 120
)

// normalizeItem trims an item and checks it: one catalog plant or one custom
// name, a positive quantity, short specifications and, when priced, prices
// that add up. Requests from customers are not priced yet.
func normalizeItem(item *QuotationItemRequest, priced bool) error {
	trim := func(v **string, max int) bool {
		if *v == nil {
			return true
		}
		t := strings.Join(strings.Fields(**v), " ")
		if t == "" {
			*v = nil
			return true
		}
		*v = &t
		return len([]rune(t)) <= max
	}
	if !trim(&item.CustomPlantName, maxCustomName) || !trim(&item.ScientificName, maxCustomName) ||
		!trim(&item.Variety, maxSpecLength) || !trim(&item.SizeDescription, maxSpecLength) ||
		!trim(&item.Grade, maxSpecLength) || !trim(&item.Description, 1000) {
		return ErrInvalidInput
	}
	custom := item.CustomPlantName != nil
	if (item.PlantID > 0) == custom || item.PlantID < 0 || item.Quantity <= 0 {
		return ErrInvalidInput // exactly one of a catalog plant or a custom name
	}
	if custom && len([]rune(*item.CustomPlantName)) < 2 {
		return ErrInvalidInput
	}
	if !custom {
		item.ScientificName = nil // the catalog's name is used
	}
	if !priced {
		return nil
	}
	if item.UnitPrice < 0 || item.TotalPrice < 0 || !validateItemMath(*item) {
		return ErrInvalidInput
	}
	return nil
}

func validateItemMath(item QuotationItemRequest) bool {
	expected := item.Quantity * item.UnitPrice
	return math.Abs(expected-item.TotalPrice) <= 0.01
}

func scanVerification(row interface{ Scan(dest ...any) error }) (*QuotationVerification, error) {
	var v QuotationVerification
	var revokedAt sql.NullTime
	var revokedBy sql.NullInt64
	err := row.Scan(&v.VerificationID, &v.QuotationID, &v.Token, &v.Status, &v.CreatedAt, &revokedAt, &revokedBy)
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
