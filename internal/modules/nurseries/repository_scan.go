// SQL filters, row scanners and NULL helpers for the nurseries repository.

package nurseries

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func buildNurseryWhere(input ListNurseriesRequest) (string, []any) {
	clauses := []string{"COALESCE(n.status::text, '') <> 'DELETED'"}
	args := make([]any, 0)
	if input.Search != "" {
		args = append(args, input.Search)
		index := len(args)
		clauses = append(clauses, fmt.Sprintf(`(n.nursery_name ILIKE '%%' || $%d || '%%' OR n.nursery_code ILIKE '%%' || $%d || '%%' OR n.description ILIKE '%%' || $%d || '%%')`, index, index, index))
	}
	if input.City != "" {
		args = append(args, input.City)
		clauses = append(clauses, fmt.Sprintf(`EXISTS (SELECT 1 FROM public.nursery_addresses na WHERE na.nursery_id = n.nursery_id AND na.city ILIKE $%d)`, len(args)))
	}
	if input.State != "" {
		args = append(args, input.State)
		clauses = append(clauses, fmt.Sprintf(`EXISTS (SELECT 1 FROM public.nursery_addresses na WHERE na.nursery_id = n.nursery_id AND na.state ILIKE $%d)`, len(args)))
	}
	status := input.NurseryStatus
	if status == "" {
		status = input.VerificationStatus
	}
	switch {
	case status == "APPROVED": // a reactivated nursery is ACTIVE: same live state
		clauses = append(clauses, `n.status::text IN ('APPROVED', 'ACTIVE')`)
	case status != "":
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf(`n.status::text = $%d`, len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *PostgresRepository) scanNursery(ctx context.Context, where string, args ...any) (*Nursery, error) {
	query := `
		SELECT n.nursery_id, n.nursery_code, n.nursery_name, n.mobile,
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.owner_user_id,
			n.created_at, n.updated_at, n.created_by_user_id, n.updated_by_user_id, n.rejection_reason, n.rejected_at,
			n.logo_url, n.brand_icon_key, n.brand_color,
			(SELECT NULLIF(TRIM(CONCAT_WS(' ', ou.first_name, ou.last_name)), '') FROM public.users ou WHERE ou.user_id = n.owner_user_id)
		FROM public.nurseries n
		` + where
	nursery, err := scanNurseryRow(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return nursery, err
}

func scanNurseryRow(row interface{ Scan(dest ...any) error }) (*Nursery, error) {
	nursery, err := scanNursery(row)
	if err != nil {
		return nil, err
	}
	return &nursery, nil
}

func scanNurseryRows(rows *sql.Rows) (Nursery, error) {
	return scanNursery(rows)
}

func scanNursery(row interface{ Scan(dest ...any) error }) (Nursery, error) {
	var nursery Nursery
	var code, mobile, email, website, description, rejectionReason sql.NullString
	var logoURL, brandIconKey, brandColor, ownerName sql.NullString
	var ownerUserID, createdBy, updatedBy sql.NullInt64
	var rejectedAt sql.NullTime
	if err := row.Scan(
		&nursery.ID, &code, &nursery.Name, &mobile, &email, &website, &description,
		&nursery.Status, &ownerUserID, &nursery.CreatedAt, &nursery.UpdatedAt, &createdBy, &updatedBy,
		&rejectionReason, &rejectedAt,
		&logoURL, &brandIconKey, &brandColor, &ownerName,
	); err != nil {
		return Nursery{}, err
	}
	nursery.OwnerName = nullableString(ownerName)
	nursery.Code = nullableString(code)
	nursery.NurseryCode = nullableString(code)
	nursery.Mobile = nullableString(mobile)
	nursery.Email = nullableString(email)
	nursery.Website = nullableString(website)
	nursery.Description = nullableString(description)
	nursery.RejectionReason = nullableString(rejectionReason)
	if rejectedAt.Valid {
		nursery.RejectedAt = &rejectedAt.Time
	}
	nursery.OwnerUserID = nullableInt64(ownerUserID)
	nursery.CreatedBy = nullableInt64(createdBy)
	nursery.UpdatedBy = nullableInt64(updatedBy)
	nursery.LogoURL = nullableString(logoURL)
	nursery.BrandIconKey = nullableString(brandIconKey)
	nursery.BrandColor = nullableString(brandColor)
	return nursery, nil
}

func scanAddressRow(row interface{ Scan(dest ...any) error }) (*Address, error) {
	address, err := scanAddress(row)
	if err != nil {
		return nil, err
	}
	return &address, nil
}

func scanAddressRows(rows *sql.Rows) (Address, error) {
	return scanAddress(rows)
}

func scanAddress(row interface{ Scan(dest ...any) error }) (Address, error) {
	var address Address
	var addressType, line1, line2, city, state, country, postal sql.NullString
	var landmark, locationSource sql.NullString
	var lat, lon, gpsAccuracy sql.NullFloat64
	var confirmedBy sql.NullInt64
	var confirmedAt, createdAt, updatedAt sql.NullTime
	if err := row.Scan(
		&address.ID,
		&address.NurseryID,
		&addressType,
		&line1,
		&line2,
		&city,
		&state,
		&country,
		&postal,
		&lat,
		&lon,
		&address.IsPrimary,
		&gpsAccuracy,
		&landmark,
		&locationSource,
		&confirmedBy,
		&confirmedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Address{}, err
	}
	address.AddressType = nullableString(addressType)
	address.AddressLine1 = nullableString(line1)
	address.AddressLine2 = nullableString(line2)
	address.City = nullableString(city)
	address.State = nullableString(state)
	address.Country = nullableString(country)
	address.PostalCode = nullableString(postal)
	address.Latitude = nullableFloat64(lat)
	address.Longitude = nullableFloat64(lon)
	address.GPSAccuracyM = nullableFloat64(gpsAccuracy)
	address.Landmark = nullableString(landmark)
	address.LocationSource = nullableString(locationSource)
	address.LocationConfirmedBy = nullableInt64(confirmedBy)
	if confirmedAt.Valid {
		address.LocationConfirmedAt = &confirmedAt.Time
	}
	if createdAt.Valid {
		address.CreatedAt = &createdAt.Time
	}
	if updatedAt.Valid {
		address.UpdatedAt = &updatedAt.Time
	}
	return address, nil
}

func scanManagerLinkRows(rows *sql.Rows) (UserLink, error) {
	return scanManagerLink(rows)
}

func scanManagerLink(row interface{ Scan(dest ...any) error }) (UserLink, error) {
	var user UserLink
	var lastName, fullName, email sql.NullString
	var joinedAt sql.NullTime
	// columns: id, nursery_id, user_id, first_name, last_name, full_name, mobile, email,
	//          role_id(legacy), role_code, role_name, role(v1), status, joined_at, is_active
	if err := row.Scan(&user.ID, &user.NurseryID, &user.UserID, &user.FirstName, &lastName, &fullName, &user.Mobile, &email,
		&user.RoleCode, &user.RoleName, &user.Role, &user.Status, &joinedAt, &user.IsActive); err != nil {
		return UserLink{}, err
	}
	user.LastName = nullableString(lastName)
	if fullName.Valid && strings.TrimSpace(fullName.String) != "" {
		user.FullName = fullName.String
	} else {
		user.FullName = user.FirstName
	}
	user.Email = nullableString(email)
	if joinedAt.Valid {
		user.JoinedAt = &joinedAt.Time
	}
	return user, nil
}

func statusOrActive(value *string) string {
	status := strings.ToUpper(strings.TrimSpace(stringOrEmpty(value)))
	if status == "" {
		return "ACTIVE"
	}
	return status
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

func floatOrNil(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func int64OrNil(value *int64) any {
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

// nullOrString returns nil when the pointer is nil or the trimmed string is empty,
// otherwise returns the trimmed string. Used for optional DB columns.
func nullOrString(value *string) any {
	if value == nil {
		return nil
	}
	s := strings.TrimSpace(*value)
	if s == "" {
		return nil
	}
	return s
}

// queryIDs runs a query returning one bigint column and closes its rows
// before returning, so the same transaction can run the next statement.
func queryIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
