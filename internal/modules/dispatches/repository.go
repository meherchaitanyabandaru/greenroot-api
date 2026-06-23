package dispatches

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	List(ctx context.Context, input ListDispatchesRequest) ([]Dispatch, int64, error)
	FindByID(ctx context.Context, dispatchID int64) (*Dispatch, error)
	HasDuplicate(ctx context.Context, dispatchNumber string) (bool, error)
	Create(ctx context.Context, actorID int64, input CreateDispatchInput) (*Dispatch, error)
	UpdateStatus(ctx context.Context, dispatchID int64, input UpdateStatusInput) (*Dispatch, error)
	CreateItem(ctx context.Context, dispatchID int64, input DispatchItemRequest) (*DispatchItem, error)
	ListItems(ctx context.Context, dispatchID int64) ([]DispatchItem, error)
	OrderAccess(ctx context.Context, orderID int64) (*OrderAccess, error)
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
}

type CreateDispatchInput struct {
	OrderID            int64
	DispatchNumber     *string
	VehicleID          *int64
	DriverID           *int64
	DispatchDate       *time.Time
	DestinationAddress *string
	Notes              *string
	Items              []DispatchItemRequest
}

type UpdateStatusInput struct {
	Status       string
	DeliveryDate *time.Time
	Notes        *string
}

type CreateAuditInput struct {
	TableName string
	RecordID  int64
	Action    string
	ChangedBy int64
	SourceIP  string
	UserAgent string
	NewJSON   string
	At        time.Time
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListDispatchesRequest) ([]Dispatch, int64, error) {
	where, args := buildWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, baseCount()+" "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(baseSelect()+`
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, where, sortClause(input), len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	dispatches := make([]Dispatch, 0)
	for rows.Next() {
		dispatch, err := scanDispatch(rows)
		if err != nil {
			return nil, 0, err
		}
		dispatches = append(dispatches, dispatch)
	}
	return dispatches, total, rows.Err()
}

func (r *PostgresRepository) HasDuplicate(ctx context.Context, dispatchNumber string) (bool, error) {
	if strings.TrimSpace(dispatchNumber) == "" {
		return false, nil
	}
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM public.dispatches
			WHERE UPPER(COALESCE(dispatch_number, '')) = UPPER($1)
				AND COALESCE(dispatch_status, '') <> 'CANCELLED'
		)
	`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(dispatchNumber)).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) FindByID(ctx context.Context, dispatchID int64) (*Dispatch, error) {
	dispatch, err := scanDispatch(r.db.QueryRowContext(ctx, baseSelect()+" WHERE d.dispatch_id = $1", dispatchID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	dispatch.Items, _ = r.ListItems(ctx, dispatch.ID)
	return &dispatch, nil
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input CreateDispatchInput) (*Dispatch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now()
	dispatchCode, err := publiccode.Next(ctx, tx, publiccode.Dispatches, now)
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.dispatches (
			dispatch_code, order_id, dispatch_number, dispatch_status, vehicle_id, driver_id, dispatched_by,
			dispatch_date, destination_address, notes, created_at, updated_at
		)
		VALUES ($1, $2, NULLIF($3, ''), 'PENDING', $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, $10)
		RETURNING dispatch_id
	`
	var dispatchID int64
	if err := tx.QueryRowContext(ctx, query, dispatchCode, input.OrderID, stringOrEmpty(input.DispatchNumber), int64OrNil(input.VehicleID), int64OrNil(input.DriverID), actorID, timeOrNil(input.DispatchDate), stringOrEmpty(input.DestinationAddress), stringOrEmpty(input.Notes), now).Scan(&dispatchID); err != nil {
		return nil, err
	}
	for _, item := range input.Items {
		if _, err := r.createItemTx(ctx, tx, dispatchID, item); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, dispatchID)
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, dispatchID int64, input UpdateStatusInput) (*Dispatch, error) {
	const query = `
		UPDATE public.dispatches
		SET dispatch_status = $2,
			delivery_date = CASE WHEN $5 = 'DELIVERED' THEN COALESCE($3, CURRENT_TIMESTAMP) ELSE COALESCE($3, delivery_date) END,
			notes = COALESCE(NULLIF($4, ''), notes),
			updated_at = CURRENT_TIMESTAMP
		WHERE dispatch_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, dispatchID, input.Status, timeOrNil(input.DeliveryDate), stringOrEmpty(input.Notes), input.Status)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, dispatchID)
}

func (r *PostgresRepository) CreateItem(ctx context.Context, dispatchID int64, input DispatchItemRequest) (*DispatchItem, error) {
	return r.createItemTx(ctx, r.db, dispatchID, input)
}

func (r *PostgresRepository) createItemTx(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, dispatchID int64, input DispatchItemRequest) (*DispatchItem, error) {
	const query = `
		INSERT INTO public.dispatch_items (dispatch_id, order_item_id, quantity, notes, created_at, updated_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING dispatch_item_id, dispatch_id, order_item_id, quantity, notes, created_at
	`
	item, err := scanItem(q.QueryRowContext(ctx, query, dispatchID, int64OrNil(input.OrderItemID), input.Quantity, stringOrEmpty(input.Notes)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &item, err
}

func (r *PostgresRepository) ListItems(ctx context.Context, dispatchID int64) ([]DispatchItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT di.dispatch_item_id, di.dispatch_id, di.order_item_id, oi.plant_id, p.scientific_name,
			di.quantity, di.notes, di.created_at
		FROM public.dispatch_items di
		LEFT JOIN public.order_items oi ON oi.order_item_id = di.order_item_id
		LEFT JOIN public.plants p ON p.plant_id = oi.plant_id
		WHERE di.dispatch_id = $1
		ORDER BY di.dispatch_item_id
	`, dispatchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DispatchItem, 0)
	for rows.Next() {
		item, err := scanJoinedItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) OrderAccess(ctx context.Context, orderID int64) (*OrderAccess, error) {
	var access OrderAccess
	var buyerID, nurseryID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT order_id, buyer_user_id, seller_nursery_id FROM public.orders WHERE order_id = $1`, orderID).Scan(&access.OrderID, &buyerID, &nurseryID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	access.BuyerID = nullableInt64(buyerID)
	access.NurseryID = nullableInt64(nurseryID)
	return &access, nil
}

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.nursery_users WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true)`, nurseryID, userID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, input CreateAuditInput) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO public.audit_logs (table_name, record_id, action_type, old_data, new_data, changed_by, source_ip, user_agent, changed_at)
		VALUES ($1, $2, $3, NULL, NULLIF($4, '')::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
	`, input.TableName, input.RecordID, input.Action, input.NewJSON, input.ChangedBy, input.SourceIP, input.UserAgent, input.At)
	return err
}

func baseSelect() string {
	return `
		SELECT d.dispatch_id, d.dispatch_code, d.order_id, o.order_number, o.seller_nursery_id, d.dispatch_number,
			COALESCE(d.dispatch_status::text, ''), d.vehicle_id, v.vehicle_number, d.driver_id,
			u.first_name, d.dispatched_by, d.dispatch_date, d.delivery_date, d.destination_address,
			d.notes, d.created_at, d.updated_at
		FROM public.dispatches d
		JOIN public.orders o ON o.order_id = d.order_id
		LEFT JOIN public.vehicles v ON v.vehicle_id = d.vehicle_id
		LEFT JOIN public.drivers dr ON dr.driver_id = d.driver_id
		LEFT JOIN public.users u ON u.user_id = dr.user_id
	`
}

func baseCount() string {
	return `
		SELECT COUNT(*)
		FROM public.dispatches d
		JOIN public.orders o ON o.order_id = d.order_id
		LEFT JOIN public.vehicles v ON v.vehicle_id = d.vehicle_id
		LEFT JOIN public.drivers dr ON dr.driver_id = d.driver_id
		LEFT JOIN public.users u ON u.user_id = dr.user_id
	`
}

func buildWhere(input ListDispatchesRequest) (string, []any) {
	clauses := []string{"1 = 1"}
	args := make([]any, 0)
	if input.OrderID > 0 {
		args = append(args, input.OrderID)
		clauses = append(clauses, fmt.Sprintf("d.order_id = $%d", len(args)))
	}
	if input.NurseryID > 0 {
		args = append(args, input.NurseryID)
		clauses = append(clauses, fmt.Sprintf("o.seller_nursery_id = $%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("d.dispatch_status::text = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(d.dispatch_code ILIKE $%d OR d.dispatch_number ILIKE $%d OR o.order_number ILIKE $%d OR v.vehicle_number ILIKE $%d OR u.first_name ILIKE $%d)", len(args), len(args), len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListDispatchesRequest) string {
	direction := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		direction = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "id":
		return "d.dispatch_id " + direction
	case "dispatch_code":
		return "d.dispatch_code " + direction + " NULLS LAST, d.dispatch_id DESC"
	case "dispatch_number":
		return "d.dispatch_number " + direction + " NULLS LAST, d.dispatch_id DESC"
	case "dispatch_status", "status":
		return "d.dispatch_status " + direction + " NULLS LAST, d.dispatch_id DESC"
	case "order_number":
		return "o.order_number " + direction + " NULLS LAST, d.dispatch_id DESC"
	case "vehicle_number":
		return "v.vehicle_number " + direction + " NULLS LAST, d.dispatch_id DESC"
	case "driver_name":
		return "u.first_name " + direction + " NULLS LAST, d.dispatch_id DESC"
	case "dispatch_date":
		return "d.dispatch_date " + direction + " NULLS LAST, d.dispatch_id DESC"
	case "created_at":
		return "d.created_at " + direction + " NULLS LAST, d.dispatch_id DESC"
	default:
		return "d.dispatch_id DESC"
	}
}

func scanDispatch(row interface{ Scan(dest ...any) error }) (Dispatch, error) {
	var dispatch Dispatch
	var orderNumber, dispatchNumber, vehicleNumber, driverName, destination, notes sql.NullString
	var nurseryID, vehicleID, driverID, dispatchedBy sql.NullInt64
	var dispatchDate, deliveryDate, updatedAt sql.NullTime
	if err := row.Scan(&dispatch.ID, &dispatch.DispatchCode, &dispatch.OrderID, &orderNumber, &nurseryID, &dispatchNumber, &dispatch.Status, &vehicleID, &vehicleNumber, &driverID, &driverName, &dispatchedBy, &dispatchDate, &deliveryDate, &destination, &notes, &dispatch.CreatedAt, &updatedAt); err != nil {
		return Dispatch{}, err
	}
	dispatch.OrderNumber = nullableString(orderNumber)
	dispatch.SellerNurseryID = nullableInt64(nurseryID)
	dispatch.DispatchNumber = nullableString(dispatchNumber)
	dispatch.VehicleID = nullableInt64(vehicleID)
	dispatch.VehicleNumber = nullableString(vehicleNumber)
	dispatch.DriverID = nullableInt64(driverID)
	dispatch.DriverName = nullableString(driverName)
	dispatch.DispatchedBy = nullableInt64(dispatchedBy)
	if dispatchDate.Valid {
		dispatch.DispatchDate = &dispatchDate.Time
	}
	if deliveryDate.Valid {
		dispatch.DeliveryDate = &deliveryDate.Time
	}
	dispatch.DestinationAddress = nullableString(destination)
	dispatch.Notes = nullableString(notes)
	if updatedAt.Valid {
		dispatch.UpdatedAt = &updatedAt.Time
	}
	return dispatch, nil
}

func scanItem(row interface{ Scan(dest ...any) error }) (DispatchItem, error) {
	var item DispatchItem
	var orderItemID sql.NullInt64
	var notes sql.NullString
	if err := row.Scan(&item.ID, &item.DispatchID, &orderItemID, &item.Quantity, &notes, &item.CreatedAt); err != nil {
		return DispatchItem{}, err
	}
	item.OrderItemID = nullableInt64(orderItemID)
	item.Notes = nullableString(notes)
	return item, nil
}

func scanJoinedItem(row interface{ Scan(dest ...any) error }) (DispatchItem, error) {
	var item DispatchItem
	var orderItemID, plantID sql.NullInt64
	var plantName, notes sql.NullString
	if err := row.Scan(&item.ID, &item.DispatchID, &orderItemID, &plantID, &plantName, &item.Quantity, &notes, &item.CreatedAt); err != nil {
		return DispatchItem{}, err
	}
	item.OrderItemID = nullableInt64(orderItemID)
	item.PlantID = nullableInt64(plantID)
	item.PlantName = nullableString(plantName)
	item.Notes = nullableString(notes)
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
func timeOrNil(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
