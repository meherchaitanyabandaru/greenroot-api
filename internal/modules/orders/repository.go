package orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var ErrNotFound     = apperrs.ErrNotFound

type Repository interface {
	List(ctx context.Context, input ListOrdersRequest) ([]Order, int64, error)
	FindByID(ctx context.Context, orderID int64) (*Order, error)
	Create(ctx context.Context, actorID int64, input CreateOrderRequest, orderNumber string) (*Order, error)
	GetDeliverySnapshot(ctx context.Context, orderID int64) (*DeliverySnapshot, error)
	UpdateDeliverySnapshot(ctx context.Context, orderID int64, actorID int64, input DeliverySnapshotRequest) (*DeliverySnapshot, error)
	OrderHasStartedDispatch(ctx context.Context, orderID int64) (bool, error)
	OrderHasUndeliveredDispatch(ctx context.Context, orderID int64) (bool, error)
	ActiveDispatchForOrder(ctx context.Context, orderID int64) (*ActiveDispatchSummary, error)
	BatchActiveDispatchForOrders(ctx context.Context, orderIDs []int64) (map[int64]*ActiveDispatchSummary, error)
	StartedDispatchDriverUserID(ctx context.Context, orderID int64) (*int64, error)
	UpdateStatus(ctx context.Context, actorID int64, orderID int64, status string) (*Order, error)
	UpdateStatusWithLoading(ctx context.Context, actorID int64, orderID int64, status string, phase string) (*Order, error)
	Cancel(ctx context.Context, actorID int64, orderID int64, reason string) (*Order, error)
	AssignManager(ctx context.Context, orderID int64, managerUserID int64) (*Order, error)
	Delete(ctx context.Context, orderID int64) error
	ListItems(ctx context.Context, orderID int64) ([]OrderItem, error)
	FindItem(ctx context.Context, itemID int64) (*OrderItem, error)
	CreateItem(ctx context.Context, orderID int64, input OrderItemRequest) (*OrderItem, error)
	UpdateItem(ctx context.Context, itemID int64, input OrderItemRequest) (*OrderItem, error)
	DeleteItem(ctx context.Context, itemID int64) error
	SetLoadedQuantity(ctx context.Context, itemID int64, qty float64) (*OrderItem, error)
	RecalculateTotalFromLoaded(ctx context.Context, orderID int64) error
	CreateNotification(ctx context.Context, userID int64, notifType, title, message string) error
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	GetUserNurseryIDs(ctx context.Context, userID int64) ([]int64, error)
	GetOwnedNurseryID(ctx context.Context, userID int64) (*int64, error)
	FindOrCreateBuyerByMobile(ctx context.Context, mobile string, name string) (int64, error)
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) List(ctx context.Context, input ListOrdersRequest) ([]Order, int64, error) {
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

	orders := make([]Order, 0)
	for rows.Next() {
		order, err := scanOrderRows(rows)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, order)
	}
	return orders, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, orderID int64) (*Order, error) {
	order, err := scanOrderRow(r.db.QueryRowContext(ctx, baseSelect()+" WHERE o.order_id = $1", orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	order.Items, _ = r.ListItems(ctx, order.ID)
	order.DeliverySnapshot, _ = r.GetDeliverySnapshot(ctx, order.ID)
	return order, nil
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input CreateOrderRequest, orderNumber string) (*Order, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now()
	orderCode, err := publiccode.Next(ctx, tx, publiccode.Orders, now)
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.orders (
			order_code, order_number, buyer_user_id, seller_nursery_id, buyer_nursery_id, order_status, total_amount,
			notes, order_date, created_at, updated_at, created_by, updated_by
		)
		VALUES ($1, $2, $3, $4, $9, $5, 0, NULLIF($6, ''), $7, $7, $7, $8, $8)
		RETURNING order_id
	`
	var orderID int64
	if err := tx.QueryRowContext(
		ctx,
		query,
		orderCode,
		orderNumber,
		int64OrNil(input.BuyerUserID),
		int64OrNil(input.SellerNurseryID),
		statusOrPending(input.Status),
		stringOrEmpty(input.Notes),
		now,
		actorID,
		int64OrNil(input.BuyerNurseryID),
	).Scan(&orderID); err != nil {
		return nil, err
	}

	for _, item := range input.Items {
		if _, err := r.createItemTx(ctx, tx, orderID, item); err != nil {
			return nil, err
		}
	}
	if err := r.refreshTotalTx(ctx, tx, orderID); err != nil {
		return nil, err
	}
	if err := r.createDeliverySnapshotTx(ctx, tx, orderID, actorID, input); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, orderID)
}

func (r *PostgresRepository) createDeliverySnapshotTx(ctx context.Context, tx *sql.Tx, orderID int64, actorID int64, input CreateOrderRequest) error {
	if input.Delivery != nil {
		return r.upsertDeliverySnapshot(ctx, tx, orderID, actorID, *input.Delivery)
	}
	const query = `
		INSERT INTO public.order_delivery_snapshots (
			order_id, contact_name, contact_mobile, address_line1, address_line2,
			city, state, country, postal_code, landmark, latitude, longitude,
			location, gps_accuracy_meters, location_source, confirmed_by, confirmed_at
		)
		SELECT $1, ua.contact_name, ua.contact_mobile, ua.address_line1, ua.address_line2,
			ua.city, ua.state, ua.country, ua.postal_code, ua.landmark, ua.latitude, ua.longitude,
			CASE WHEN ua.latitude IS NOT NULL AND ua.longitude IS NOT NULL
				THEN ST_SetSRID(ST_MakePoint(ua.longitude::double precision, ua.latitude::double precision), 4326)::geography
				ELSE NULL
			END,
			ua.gps_accuracy_meters, COALESCE(ua.location_source, 'address_search'), $2, CURRENT_TIMESTAMP
		FROM public.user_addresses ua
		WHERE ua.user_id = $3
		ORDER BY ua.is_default DESC, ua.address_id DESC
		LIMIT 1
		ON CONFLICT (order_id) DO NOTHING
	`
	_, err := tx.ExecContext(ctx, query, orderID, actorID, int64OrNil(input.BuyerUserID))
	return err
}

func (r *PostgresRepository) GetDeliverySnapshot(ctx context.Context, orderID int64) (*DeliverySnapshot, error) {
	const query = `
		SELECT snapshot_id, order_id, contact_name, contact_mobile, alternate_mobile,
			address_line1, address_line2, city, state, country, postal_code, landmark,
			delivery_instructions, latitude, longitude, gps_accuracy_meters, location_source,
			confirmed_by, confirmed_at, emergency_updated, requires_driver_ack,
			driver_acknowledged_by, driver_acknowledged_at, created_at, updated_at
		FROM public.order_delivery_snapshots
		WHERE order_id = $1
	`
	snapshot, err := scanDeliverySnapshot(r.db.QueryRowContext(ctx, query, orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *PostgresRepository) UpdateDeliverySnapshot(ctx context.Context, orderID int64, actorID int64, input DeliverySnapshotRequest) (*DeliverySnapshot, error) {
	input.ConfirmedBy = &actorID
	now := time.Now()
	input.ConfirmedAt = &now
	if err := r.upsertDeliverySnapshot(ctx, r.db, orderID, actorID, input); err != nil {
		return nil, err
	}
	return r.GetDeliverySnapshot(ctx, orderID)
}

func (r *PostgresRepository) upsertDeliverySnapshot(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, orderID int64, actorID int64, input DeliverySnapshotRequest) error {
	const query = `
		INSERT INTO public.order_delivery_snapshots (
			order_id, contact_name, contact_mobile, alternate_mobile, address_line1, address_line2,
			city, state, country, postal_code, landmark, delivery_instructions,
			latitude, longitude, location, gps_accuracy_meters, location_source,
			confirmed_by, confirmed_at, emergency_updated, requires_driver_ack,
			created_at, updated_at
		)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), NULLIF($6,''),
			NULLIF($7,''), NULLIF($8,''), NULLIF($9,''), NULLIF($10,''), NULLIF($11,''), NULLIF($12,''),
			$13, $14,
			CASE WHEN $13::numeric IS NOT NULL AND $14::numeric IS NOT NULL
				THEN ST_SetSRID(ST_MakePoint($14::double precision, $13::double precision), 4326)::geography
				ELSE NULL
			END,
			$15, NULLIF($16,''), $17, $18, $19, $19, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (order_id) DO UPDATE SET
			contact_name = EXCLUDED.contact_name,
			contact_mobile = EXCLUDED.contact_mobile,
			alternate_mobile = EXCLUDED.alternate_mobile,
			address_line1 = EXCLUDED.address_line1,
			address_line2 = EXCLUDED.address_line2,
			city = EXCLUDED.city,
			state = EXCLUDED.state,
			country = EXCLUDED.country,
			postal_code = EXCLUDED.postal_code,
			landmark = EXCLUDED.landmark,
			delivery_instructions = EXCLUDED.delivery_instructions,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			location = EXCLUDED.location,
			gps_accuracy_meters = EXCLUDED.gps_accuracy_meters,
			location_source = EXCLUDED.location_source,
			confirmed_by = EXCLUDED.confirmed_by,
			confirmed_at = EXCLUDED.confirmed_at,
			emergency_updated = order_delivery_snapshots.emergency_updated OR EXCLUDED.emergency_updated,
			requires_driver_ack = order_delivery_snapshots.requires_driver_ack OR EXCLUDED.requires_driver_ack,
			driver_acknowledged_by = CASE WHEN EXCLUDED.requires_driver_ack THEN NULL ELSE order_delivery_snapshots.driver_acknowledged_by END,
			driver_acknowledged_at = CASE WHEN EXCLUDED.requires_driver_ack THEN NULL ELSE order_delivery_snapshots.driver_acknowledged_at END,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := q.ExecContext(ctx, query,
		orderID,
		stringOrEmpty(input.ContactName),
		stringOrEmpty(input.ContactMobile),
		stringOrEmpty(input.AlternateMobile),
		stringOrEmpty(input.AddressLine1),
		stringOrEmpty(input.AddressLine2),
		stringOrEmpty(input.City),
		stringOrEmpty(input.State),
		stringOrEmpty(input.Country),
		stringOrEmpty(input.PostalCode),
		stringOrEmpty(input.Landmark),
		stringOrEmpty(input.DeliveryInstructions),
		floatOrNil(input.Latitude),
		floatOrNil(input.Longitude),
		floatOrNil(input.GPSAccuracyM),
		stringOrEmpty(input.LocationSource),
		int64OrNil(input.ConfirmedBy),
		timeOrNil(input.ConfirmedAt),
		input.EmergencyUpdate,
	)
	return err
}

func (r *PostgresRepository) OrderHasStartedDispatch(ctx context.Context, orderID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.dispatches
			WHERE order_id = $1
			  AND (
				trip_started_at IS NOT NULL
				OR dispatch_status IN ('IN_TRANSIT', 'DELIVERED')
			  )
		)
	`, orderID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) OrderHasUndeliveredDispatch(ctx context.Context, orderID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.dispatches
			WHERE order_id = $1
			  AND COALESCE(dispatch_status, '') <> 'CANCELLED'
			  AND COALESCE(dispatch_status, '') <> 'DELIVERED'
		)
	`, orderID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) ActiveDispatchForOrder(ctx context.Context, orderID int64) (*ActiveDispatchSummary, error) {
	var summary ActiveDispatchSummary
	err := r.db.QueryRowContext(ctx, `
		SELECT dispatch_id, dispatch_status::text
		FROM public.dispatches
		WHERE order_id = $1
		  AND COALESCE(dispatch_status::text, '') <> 'CANCELLED'
		ORDER BY
		  CASE COALESCE(dispatch_status::text, '')
			WHEN 'DELIVERED' THEN 5
			WHEN 'IN_TRANSIT' THEN 4
			WHEN 'DISPATCHED' THEN 3
			WHEN 'ACCEPTED' THEN 2
			WHEN 'PENDING' THEN 1
			ELSE 0
		  END DESC,
		  updated_at DESC NULLS LAST,
		  delivery_date DESC NULLS LAST,
		  dispatch_date DESC NULLS LAST,
		  created_at DESC,
		  dispatch_id DESC
		LIMIT 1
	`, orderID).Scan(&summary.ID, &summary.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (r *PostgresRepository) BatchActiveDispatchForOrders(ctx context.Context, orderIDs []int64) (map[int64]*ActiveDispatchSummary, error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(orderIDs))
	args := make([]any, len(orderIDs))
	for i, id := range orderIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT DISTINCT ON (order_id) order_id, dispatch_id, dispatch_status::text
		FROM public.dispatches
		WHERE order_id IN (%s)
		  AND COALESCE(dispatch_status::text, '') <> 'CANCELLED'
		ORDER BY order_id,
		  CASE COALESCE(dispatch_status::text, '')
			WHEN 'DELIVERED' THEN 5
			WHEN 'IN_TRANSIT' THEN 4
			WHEN 'DISPATCHED' THEN 3
			WHEN 'ACCEPTED' THEN 2
			WHEN 'PENDING' THEN 1
			ELSE 0
		  END DESC,
		  updated_at DESC NULLS LAST,
		  dispatch_id DESC
	`, strings.Join(placeholders, ","))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]*ActiveDispatchSummary)
	for rows.Next() {
		var orderID int64
		var s ActiveDispatchSummary
		if err := rows.Scan(&orderID, &s.ID, &s.Status); err != nil {
			return nil, err
		}
		s2 := s
		result[orderID] = &s2
	}
	return result, rows.Err()
}

func (r *PostgresRepository) StartedDispatchDriverUserID(ctx context.Context, orderID int64) (*int64, error) {
	var userID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(d.driver_user_id, nd.driver_user_id)
		FROM public.dispatches d
		LEFT JOIN public.nursery_drivers nd ON nd.driver_id = d.driver_id
		WHERE d.order_id = $1
		  AND COALESCE(d.dispatch_status, '') IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')
		  AND COALESCE(d.driver_user_id, nd.driver_user_id) IS NOT NULL
		ORDER BY d.updated_at DESC, d.dispatch_id DESC
		LIMIT 1
	`, orderID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nullableInt64(userID), nil
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, actorID int64, orderID int64, status string) (*Order, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE public.orders SET order_status = $2, updated_at = CURRENT_TIMESTAMP, updated_by = $3 WHERE order_id = $1`, orderID, status, actorID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, orderID)
}

func (r *PostgresRepository) Delete(ctx context.Context, orderID int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM public.orders WHERE order_id = $1`, orderID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListItems(ctx context.Context, orderID int64) ([]OrderItem, error) {
	rows, err := r.db.QueryContext(ctx, itemSelect()+" WHERE oi.order_id = $1 ORDER BY oi.order_item_id", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]OrderItem, 0)
	for rows.Next() {
		item, err := scanItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) FindItem(ctx context.Context, itemID int64) (*OrderItem, error) {
	return r.findItem(ctx, itemID)
}

func (r *PostgresRepository) CreateItem(ctx context.Context, orderID int64, input OrderItemRequest) (*OrderItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	itemID, err := r.createItemTx(ctx, tx, orderID, input)
	if err != nil {
		return nil, err
	}
	if err := r.refreshTotalTx(ctx, tx, orderID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.findItem(ctx, itemID)
}

func (r *PostgresRepository) UpdateItem(ctx context.Context, itemID int64, input OrderItemRequest) (*OrderItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var orderID int64
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM public.order_items WHERE order_item_id = $1`, itemID).Scan(&orderID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}

	const query = `
		UPDATE public.order_items
		SET plant_id = $2,
			size_id = $3,
			quantity = $4,
			unit_price = $5,
			total_price = $6,
			remarks = NULLIF($7, '')
		WHERE order_item_id = $1
		RETURNING order_item_id, order_id, plant_id, size_id, quantity, unit_price, total_price, remarks, created_at
	`
	var raw rawOrderItem
	if err := tx.QueryRowContext(
		ctx,
		query,
		itemID,
		input.PlantID,
		int16OrNil(input.SizeID),
		input.Quantity,
		input.UnitPrice,
		input.TotalPrice,
		stringOrEmpty(input.Remarks),
	).Scan(&raw.ID, &raw.OrderID, &raw.PlantID, &raw.SizeID, &raw.Quantity, &raw.UnitPrice, &raw.TotalPrice, &raw.Remarks, &raw.CreatedAt); err != nil {
		return nil, err
	}
	if err := r.refreshTotalTx(ctx, tx, orderID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.findItem(ctx, raw.ID)
}

func (r *PostgresRepository) DeleteItem(ctx context.Context, itemID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID int64
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM public.order_items WHERE order_item_id = $1`, itemID).Scan(&orderID); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM public.order_items WHERE order_item_id = $1`, itemID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	if err := r.refreshTotalTx(ctx, tx, orderID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM public.nursery_users
		WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true
		UNION ALL
		SELECT 1 FROM public.nurseries
		WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text, '') <> 'DELETED'
	)`, nurseryID, userID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND owner_user_id = $2)`,
		nurseryID, userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) GetUserNurseryIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT nursery_id FROM (
			SELECT nursery_id FROM public.nurseries
				WHERE owner_user_id = $1 AND COALESCE(status::text, '') <> 'DELETED'
			UNION ALL
			SELECT nursery_id FROM public.nursery_users
				WHERE user_id = $1 AND COALESCE(is_active, true) = true
		) AS combined
	`, userID)
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

func (r *PostgresRepository) GetOwnedNurseryID(ctx context.Context, userID int64) (*int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		SELECT nursery_id FROM public.nurseries
		WHERE owner_user_id = $1 AND COALESCE(status::text, '') <> 'DELETED'
		LIMIT 1
	`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *PostgresRepository) UpdateStatusWithLoading(ctx context.Context, actorID int64, orderID int64, status string, phase string) (*Order, error) {
	var query string
	switch phase {
	case "start":
		query = `UPDATE public.orders SET order_status = $2, loading_started_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE order_id = $1`
	case "complete":
		query = `UPDATE public.orders SET order_status = $2, loading_completed_at = CURRENT_TIMESTAMP, loading_completed_by_user_id = $3, updated_at = CURRENT_TIMESTAMP WHERE order_id = $1`
	default:
		query = `UPDATE public.orders SET order_status = $2, updated_at = CURRENT_TIMESTAMP WHERE order_id = $1`
	}
	if phase == "complete" {
		if _, err := r.db.ExecContext(ctx, query, orderID, status, actorID); err != nil {
			return nil, err
		}
	} else {
		if _, err := r.db.ExecContext(ctx, query, orderID, status); err != nil {
			return nil, err
		}
	}
	return r.FindByID(ctx, orderID)
}

func (r *PostgresRepository) Cancel(ctx context.Context, actorID int64, orderID int64, reason string) (*Order, error) {
	const query = `
		UPDATE public.orders
		SET order_status = 'CANCELLED', cancelled_by_user_id = $2, cancelled_at = CURRENT_TIMESTAMP,
		    cancel_reason = NULLIF($3, ''), updated_at = CURRENT_TIMESTAMP
		WHERE order_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, orderID, actorID, reason)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, orderID)
}

func (r *PostgresRepository) AssignManager(ctx context.Context, orderID int64, managerUserID int64) (*Order, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.orders SET assigned_manager_user_id = $2, updated_at = CURRENT_TIMESTAMP WHERE order_id = $1`,
		orderID, managerUserID,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, orderID)
}

func (r *PostgresRepository) FindOrCreateBuyerByMobile(ctx context.Context, mobile string, name string) (int64, error) {
	var userID int64
	err := r.db.QueryRowContext(ctx,
		`SELECT user_id FROM public.users WHERE mobile = $1 AND deleted_at IS NULL`,
		mobile).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	userCode, err := publiccode.Next(ctx, tx, publiccode.Users, time.Now())
	if err != nil {
		return 0, err
	}

	firstName := strings.TrimSpace(name)
	if firstName == "" {
		firstName = mobile
	}

	const insertUser = `
		INSERT INTO public.users (user_code, first_name, mobile, mobile_verified, email_verified, status, created_at, updated_at)
		VALUES ($1, $2, $3, false, false, 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING user_id
	`
	if err := tx.QueryRowContext(ctx, insertUser, userCode, firstName, mobile).Scan(&userID); err != nil {
		return 0, err
	}

	const assignRole = `
		INSERT INTO public.user_roles (user_id, role_id, assigned_at)
		SELECT $1, role_id, CURRENT_TIMESTAMP FROM public.roles WHERE role_code = 'BUYER'
		ON CONFLICT DO NOTHING
	`
	if _, err := tx.ExecContext(ctx, assignRole, userID); err != nil {
		return 0, err
	}

	return userID, tx.Commit()
}

func (r *PostgresRepository) createItemTx(ctx context.Context, tx *sql.Tx, orderID int64, input OrderItemRequest) (int64, error) {
	const query = `
		INSERT INTO public.order_items (order_id, plant_id, size_id, quantity, unit_price, total_price, remarks, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), CURRENT_TIMESTAMP)
		RETURNING order_item_id
	`
	var itemID int64
	if err := tx.QueryRowContext(ctx, query, orderID, input.PlantID, int16OrNil(input.SizeID), input.Quantity, input.UnitPrice, input.TotalPrice, stringOrEmpty(input.Remarks)).Scan(&itemID); err != nil {
		return 0, err
	}
	return itemID, nil
}

func (r *PostgresRepository) refreshTotalTx(ctx context.Context, tx *sql.Tx, orderID int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE public.orders
		SET total_amount = COALESCE((SELECT SUM(total_price) FROM public.order_items WHERE order_id = $1), 0),
			updated_at = CURRENT_TIMESTAMP
		WHERE order_id = $1
	`, orderID)
	return err
}

func (r *PostgresRepository) findItem(ctx context.Context, itemID int64) (*OrderItem, error) {
	item, err := scanItemRow(r.db.QueryRowContext(ctx, itemSelect()+" WHERE oi.order_item_id = $1", itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func baseSelect() string {
	return `
		SELECT o.order_id, o.order_code, o.order_number,
			o.buyer_user_id, bu.first_name,
			o.seller_nursery_id, n.nursery_name,
			o.order_status::text, COALESCE(o.total_amount, 0), o.notes, o.order_date,
			o.created_at, o.updated_at,
			o.created_by, o.updated_by,
			o.nursery_id, o.quotation_id, o.customer_user_id, o.customer_name, o.customer_mobile,
			o.assigned_manager_user_id, o.created_by_user_id,
			o.cancelled_by_user_id, o.cancelled_at, o.cancel_reason,
			o.loading_started_at, o.loading_completed_at, o.loading_completed_by_user_id,
			o.buyer_nursery_id,
			mu.first_name
		FROM public.orders o
		LEFT JOIN public.users bu ON bu.user_id = o.buyer_user_id
		LEFT JOIN public.nurseries n ON n.nursery_id = COALESCE(o.nursery_id, o.seller_nursery_id)
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
		SELECT oi.order_item_id, oi.order_id, oi.plant_id, p.scientific_name, p.common_name,
			oi.size_id, ps.size_code, ps.display_name, oi.quantity, oi.unit_price, oi.total_price,
			oi.remarks, oi.created_at, oi.loaded_quantity
		FROM public.order_items oi
		JOIN public.plants p ON p.plant_id = oi.plant_id
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
		clauses = append(clauses, fmt.Sprintf("o.order_status::text = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(o.order_code ILIKE $%d OR o.order_number ILIKE $%d OR bu.first_name ILIKE $%d OR n.nursery_name ILIKE $%d OR o.notes ILIKE $%d)", len(args), len(args), len(args), len(args), len(args)))
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
		return "o.order_status " + direction + " NULLS LAST, o.order_id DESC"
	case "total_amount":
		return "o.total_amount " + direction + " NULLS LAST, o.order_id DESC"
	case "order_date":
		return "o.order_date " + direction + " NULLS LAST, o.order_id DESC"
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
	var nurseryID, quotationID, customerUserID sql.NullInt64
	var customerName, customerMobile sql.NullString
	var assignedManagerUserID, createdByUserID sql.NullInt64
	var cancelledByUserID sql.NullInt64
	var cancelledAt, loadingStartedAt, loadingCompletedAt sql.NullTime
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
		&nurseryID, &quotationID, &customerUserID, &customerName, &customerMobile,
		&assignedManagerUserID, &createdByUserID,
		&cancelledByUserID, &cancelledAt, &cancelReason,
		&loadingStartedAt, &loadingCompletedAt, &loadingCompletedByUserID,
		&buyerNurseryID,
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
	order.NurseryID = nullableInt64(nurseryID)
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

type rawOrderItem struct {
	ID         int64
	OrderID    int64
	PlantID    int64
	SizeID     sql.NullInt16
	Quantity   float64
	UnitPrice  float64
	TotalPrice float64
	Remarks    sql.NullString
	CreatedAt  time.Time
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
	if err := row.Scan(&item.ID, &item.OrderID, &item.PlantID, &item.ScientificName, &commonName, &sizeID, &sizeCode, &sizeName, &item.Quantity, &item.UnitPrice, &item.TotalPrice, &remarks, &item.CreatedAt, &loadedQty); err != nil {
		return OrderItem{}, err
	}
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

func (r *PostgresRepository) SetLoadedQuantity(ctx context.Context, itemID int64, qty float64) (*OrderItem, error) {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.order_items SET loaded_quantity = $2 WHERE order_item_id = $1`,
		itemID, qty,
	)
	if err != nil {
		return nil, err
	}
	return r.findItem(ctx, itemID)
}

func (r *PostgresRepository) RecalculateTotalFromLoaded(ctx context.Context, orderID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.orders SET total_amount = (
			SELECT COALESCE(SUM(COALESCE(loaded_quantity, quantity) * COALESCE(unit_price, 0)), 0)
			FROM public.order_items WHERE order_id = $1
		) WHERE order_id = $1`,
		orderID,
	)
	return err
}

func (r *PostgresRepository) CreateNotification(ctx context.Context, userID int64, notifType, title, message string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO public.notifications (user_id, notification_type, title, message, channel, notification_status)
		 VALUES ($1, $2, $3, $4, 'IN_APP', 'PENDING')`,
		userID, notifType, title, message,
	)
	return err
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
