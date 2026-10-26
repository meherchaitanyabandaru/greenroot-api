package orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

var (
	ErrNotFound = apperrs.ErrNotFound
	ErrConflict = apperrs.ErrConflict
)

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
	// UpdateStatus applies only if the order is still at expectedVersion
	// (ErrConflict otherwise), closing the read-check-write race.
	UpdateStatus(ctx context.Context, actorID int64, orderID int64, status string, expectedVersion int64) (*Order, error)
	UpdateStatusWithLoading(ctx context.Context, actorID int64, orderID int64, status string, phase string, expectedVersion int64) (*Order, error)
	Cancel(ctx context.Context, actorID int64, orderID int64, reason string, expectedVersion int64) (*Order, error)
	AssignManager(ctx context.Context, orderID int64, managerUserID int64) (*Order, error)
	Delete(ctx context.Context, orderID int64) error
	ListItems(ctx context.Context, orderID int64) ([]OrderItem, error)
	FindItem(ctx context.Context, itemID int64) (*OrderItem, error)
	CreateItem(ctx context.Context, orderID int64, input OrderItemRequest) (*OrderItem, error)
	UpdateItem(ctx context.Context, itemID int64, input OrderItemRequest) (*OrderItem, error)
	DeleteItem(ctx context.Context, itemID int64) error
	SetLoadedQuantity(ctx context.Context, itemID int64, qty float64) (*OrderItem, error)
	RecalculateTotalFromLoaded(ctx context.Context, orderID int64) error
	CreateNotification(ctx context.Context, userID int64, notifType, title, message string, data map[string]any) error
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	GetUserNurseryIDs(ctx context.Context, userID int64) ([]int64, error)
	GetOwnedNurseryID(ctx context.Context, userID int64) (*int64, error)
	FindOrCreateBuyerByMobile(ctx context.Context, mobile string, name string) (int64, error)
	// PDF rendering
	GetNurseryContactExtras(ctx context.Context, nurseryID int64) (PDFNurseryExtras, error)
	GetUserName(ctx context.Context, userID int64) (string, error)
	// Verification token methods
	GetActiveVerificationToken(ctx context.Context, orderID int64) (*OrderVerification, error)
	GetVerificationByToken(ctx context.Context, token string) (*OrderVerification, error)
	CreateVerificationToken(ctx context.Context, orderID int64, token string) (*OrderVerification, error)
	RevokeVerificationTokens(ctx context.Context, orderID int64, revokedByUserID int64) error
	// Document (S3/MinIO storage) methods
	CreateDocument(ctx context.Context, doc OrderDocument) (*OrderDocument, error)
	GetCurrentDocument(ctx context.Context, orderID int64) (*OrderDocument, error)
	MarkDocumentsNotCurrent(ctx context.Context, orderID int64) error
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
	if orderNumber == "" {
		orderNumber = orderCode // one human-readable identifier unless the caller sets its own
	}

	const query = `
		INSERT INTO public.orders (
			order_code, order_number, buyer_user_id, seller_nursery_id, buyer_nursery_id, status, total_amount,
			notes, ordered_at, created_at, updated_at, created_by_user_id, updated_by_user_id
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
			location, gps_accuracy_meters, location_source, confirmed_by_user_id, confirmed_at
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
			confirmed_by_user_id, confirmed_at, is_emergency_update, requires_driver_ack,
			driver_acknowledged_by_user_id, driver_acknowledged_at, created_at, updated_at
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
			confirmed_by_user_id, confirmed_at, is_emergency_update, requires_driver_ack,
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
			confirmed_by_user_id = EXCLUDED.confirmed_by_user_id,
			confirmed_at = EXCLUDED.confirmed_at,
			is_emergency_update = order_delivery_snapshots.is_emergency_update OR EXCLUDED.is_emergency_update,
			requires_driver_ack = order_delivery_snapshots.requires_driver_ack OR EXCLUDED.requires_driver_ack,
			driver_acknowledged_by_user_id = CASE WHEN EXCLUDED.requires_driver_ack THEN NULL ELSE order_delivery_snapshots.driver_acknowledged_by_user_id END,
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
				OR status IN ('IN_TRANSIT', 'DELIVERED')
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
			  AND COALESCE(status, '') <> 'CANCELLED'
			  AND COALESCE(status, '') <> 'DELIVERED'
		)
	`, orderID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) ActiveDispatchForOrder(ctx context.Context, orderID int64) (*ActiveDispatchSummary, error) {
	var summary ActiveDispatchSummary
	err := r.db.QueryRowContext(ctx, `
		SELECT dispatch_id, status::text
		FROM public.dispatches
		WHERE order_id = $1
		  AND COALESCE(status::text, '') <> 'CANCELLED'
		ORDER BY
		  CASE COALESCE(status::text, '')
			WHEN 'DELIVERED' THEN 5
			WHEN 'IN_TRANSIT' THEN 4
			WHEN 'DISPATCHED' THEN 3
			WHEN 'ACCEPTED' THEN 2
			WHEN 'PENDING' THEN 1
			ELSE 0
		  END DESC,
		  updated_at DESC NULLS LAST,
		  delivered_at DESC NULLS LAST,
		  dispatched_at DESC NULLS LAST,
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
	// #nosec G201 -- IN list is generated $n placeholders; ids are bound args.
	query := fmt.Sprintf(`
		SELECT DISTINCT ON (order_id) order_id, dispatch_id, status::text
		FROM public.dispatches
		WHERE order_id IN (%s)
		  AND COALESCE(status::text, '') <> 'CANCELLED'
		ORDER BY order_id,
		  CASE COALESCE(status::text, '')
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
		SELECT COALESCE(d.driver_user_id, dr.user_id)
		FROM public.dispatches d
		LEFT JOIN public.drivers dr ON dr.driver_id = d.driver_id
		WHERE d.order_id = $1
		  AND COALESCE(d.status, '') IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')
		  AND COALESCE(d.driver_user_id, dr.user_id) IS NOT NULL
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

func (r *PostgresRepository) UpdateStatus(ctx context.Context, actorID int64, orderID int64, status string, expectedVersion int64) (*Order, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.orders SET status = $2, updated_at = CURRENT_TIMESTAMP, updated_by_user_id = $3,
			confirmed_at = CASE WHEN $2::varchar = 'CONFIRMED' THEN COALESCE(confirmed_at, CURRENT_TIMESTAMP) ELSE confirmed_at END
		WHERE order_id = $1 AND version = $4`, orderID, status, actorID, expectedVersion)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	if err := r.requireOneRow(ctx, result, orderID); err != nil {
		return nil, err
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

func (r *PostgresRepository) UpdateStatusWithLoading(ctx context.Context, actorID int64, orderID int64, status string, phase string, expectedVersion int64) (*Order, error) {
	var query string
	switch phase {
	case "start":
		query = `UPDATE public.orders SET status = $2, loading_started_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP, updated_by_user_id = $3 WHERE order_id = $1 AND version = $4`
	case "complete":
		query = `UPDATE public.orders SET status = $2, loading_completed_at = CURRENT_TIMESTAMP, loading_completed_by_user_id = $3, updated_at = CURRENT_TIMESTAMP, updated_by_user_id = $3 WHERE order_id = $1 AND version = $4`
	default:
		query = `UPDATE public.orders SET status = $2, updated_at = CURRENT_TIMESTAMP, updated_by_user_id = $3 WHERE order_id = $1 AND version = $4`
	}
	result, err := r.db.ExecContext(ctx, query, orderID, status, actorID, expectedVersion)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	if err := r.requireOneRow(ctx, result, orderID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, orderID)
}

// requireOneRow turns a version-guarded UPDATE that touched nothing into
// ErrNotFound (order gone) or ErrConflict (changed since it was read).
func (r *PostgresRepository) requireOneRow(ctx context.Context, result sql.Result, orderID int64) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.orders WHERE order_id = $1)`, orderID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}
	return ErrNotFound
}

func (r *PostgresRepository) Cancel(ctx context.Context, actorID int64, orderID int64, reason string, expectedVersion int64) (*Order, error) {
	const query = `
		UPDATE public.orders
		SET status = 'CANCELLED', cancelled_by_user_id = $2, cancelled_at = CURRENT_TIMESTAMP,
		    cancel_reason = NULLIF($3, ''), updated_at = CURRENT_TIMESTAMP
		WHERE order_id = $1 AND version = $4
	`
	result, err := r.db.ExecContext(ctx, query, orderID, actorID, reason, expectedVersion)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	if err := r.requireOneRow(ctx, result, orderID); err != nil {
		return nil, err
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
		INSERT INTO public.users (user_code, first_name, mobile, is_mobile_verified, is_email_verified, status, created_at, updated_at)
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

func (r *PostgresRepository) CreateNotification(ctx context.Context, userID int64, notifType, title, message string, data map[string]any) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO public.notifications (user_id, notification_type, title, message, channel, status, data)
		 VALUES ($1, $2, $3, $4, 'IN_APP', 'PENDING', $5::jsonb)`,
		userID, notifType, title, message, notifyqueue.DataJSON(data),
	)
	return err
}

// GetNurseryContactExtras loads nursery email/phone/address for the order PDF header.
func (r *PostgresRepository) GetNurseryContactExtras(ctx context.Context, nurseryID int64) (PDFNurseryExtras, error) {
	var extras PDFNurseryExtras
	var email, phone, line1, city, state, postal, brand sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT n.email, n.mobile, na.address_line1, na.city, na.state, na.postal_code, n.brand_color
		FROM public.nurseries n
		LEFT JOIN public.nursery_addresses na ON na.nursery_id = n.nursery_id AND na.is_primary = true
		WHERE n.nursery_id = $1
	`, nurseryID).Scan(&email, &phone, &line1, &city, &state, &postal, &brand)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return extras, err
	}
	extras.Email = email.String
	extras.Phone = phone.String
	extras.Address = joinAddressParts(line1.String, city.String, state.String, postal.String)
	if brand.Valid {
		extras.BrandColor = &brand.String
	}
	return extras, nil
}

func (r *PostgresRepository) GetUserName(ctx context.Context, userID int64) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(first_name, mobile) FROM public.users WHERE user_id = $1`,
		userID,
	).Scan(&name)
	return name, err
}
