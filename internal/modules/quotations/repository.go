package quotations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrPlantNotFound = errors.New("plant not found")
)

type Repository interface {
	List(ctx context.Context, input ListQuotationsRequest) ([]Quotation, int64, error)
	FindByID(ctx context.Context, id int64) (*Quotation, error)
	Create(ctx context.Context, actorID int64, input CreateQuotationRequest, createdByName string, nurseryName *string, nurseryPhone *string) (*Quotation, error)
	Update(ctx context.Context, id int64, input UpdateQuotationRequest) (*Quotation, error)
	Approve(ctx context.Context, id int64, byUserID int64) (*Quotation, error)
	Recall(ctx context.Context, id int64) (*Quotation, error)
	// Buyer actions
	BuyerAccept(ctx context.Context, id int64, byUserID int64) (*Quotation, error)
	BuyerReject(ctx context.Context, id int64, byUserID int64, reason string) (*Quotation, error)
	GetBuyerNurseryID(ctx context.Context, quotationID int64) (*int64, error)
	SoftDelete(ctx context.Context, id int64) error
	FindNurseryOwnerID(ctx context.Context, quotationID int64) (int64, error)
	AssignManager(ctx context.Context, quotationID int64, managerUserID int64) (*Quotation, error)
	MarkConverted(ctx context.Context, quotationID int64, orderID int64, byUserID int64) error
	GetNurseryInfo(ctx context.Context, nurseryID int64) (name string, phone string, err error)
	GetUserName(ctx context.Context, userID int64) (string, error)
	GetUserMobile(ctx context.Context, userID int64) (string, error)
	GetPlantInfo(ctx context.Context, plantID int64) (scientificName string, commonName string, err error)
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	IsNurseryActive(ctx context.Context, nurseryID int64) (bool, error)
	GetOwnedNurseryID(ctx context.Context, userID int64) (*int64, error)
	GetManagerNurseryID(ctx context.Context, userID int64) (*int64, error)
	GetOrderNurseryID(ctx context.Context, orderID int64) (*int64, error)
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
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

func (r *PostgresRepository) List(ctx context.Context, input ListQuotationsRequest) ([]Quotation, int64, error) {
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
	qs := make([]Quotation, 0)
	for rows.Next() {
		q, err := scanQuotationRows(rows)
		if err != nil {
			return nil, 0, err
		}
		qs = append(qs, q)
	}
	return qs, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, id int64) (*Quotation, error) {
	q, err := scanQuotationRow(r.db.QueryRowContext(ctx, baseSelect()+" AND q.quotation_id = $1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	q.Items, _ = r.listItems(ctx, q.ID)
	return q, nil
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input CreateQuotationRequest, createdByName string, nurseryName *string, nurseryPhone *string) (*Quotation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	code, err := publiccode.Next(ctx, tx, publiccode.Quotations, time.Now())
	if err != nil {
		return nil, err
	}

	quotationType := "CUSTOMER"
	if input.QuotationType == "INTERNAL" {
		quotationType = "INTERNAL"
	}
	initialStatus := "CUSTOMER_DRAFT"
	if quotationType == "INTERNAL" {
		initialStatus = "INTERNAL_DRAFT"
	}

	const query = `
		INSERT INTO public.quotations (
			quotation_code, created_by_user_id, created_by_name,
			nursery_id, nursery_name, nursery_phone,
			recipient_name, recipient_mobile, notes,
			buyer_nursery_id,
			total_amount, status, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,0,$11,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
		RETURNING quotation_id
	`
	var id int64
	if err := tx.QueryRowContext(ctx, query,
		code,
		actorID,
		createdByName,
		int64OrNil(input.NurseryID),
		stringOrNil(nurseryName),
		stringOrNil(nurseryPhone),
		stringOrEmpty(input.RecipientName),
		stringOrEmpty(input.RecipientMobile),
		stringOrEmpty(input.Notes),
		int64OrNil(input.BuyerNurseryID),
		initialStatus,
	).Scan(&id); err != nil {
		return nil, err
	}

	for _, item := range input.Items {
		if err := r.createItemTx(ctx, tx, id, item); err != nil {
			return nil, err
		}
	}
	if err := r.refreshTotalTx(ctx, tx, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) Update(ctx context.Context, id int64, input UpdateQuotationRequest) (*Quotation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	const q = `
		UPDATE public.quotations
		SET recipient_name    = NULLIF($1,''),
		    recipient_mobile  = NULLIF($2,''),
		    notes             = NULLIF($3,''),
		    updated_at        = CURRENT_TIMESTAMP
		WHERE quotation_id = $4
	`
	res, err := tx.ExecContext(ctx, q,
		stringOrEmpty(input.RecipientName),
		stringOrEmpty(input.RecipientMobile),
		stringOrEmpty(input.Notes),
		id,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM public.quotation_items WHERE quotation_id = $1`, id); err != nil {
		return nil, err
	}
	for _, item := range input.Items {
		if err := r.createItemTx(ctx, tx, id, item); err != nil {
			return nil, err
		}
	}
	if err := r.refreshTotalTx(ctx, tx, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) SoftDelete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.quotations SET deleted_at = CURRENT_TIMESTAMP, status = 'DELETED', updated_at = CURRENT_TIMESTAMP WHERE quotation_id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) FindNurseryOwnerID(ctx context.Context, quotationID int64) (int64, error) {
	var ownerID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT n.owner_user_id
		FROM public.quotations q
		JOIN public.nurseries n ON n.nursery_id = q.nursery_id
		WHERE q.quotation_id = $1
	`, quotationID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if !ownerID.Valid {
		return 0, ErrNotFound
	}
	return ownerID.Int64, nil
}

func (r *PostgresRepository) AssignManager(ctx context.Context, quotationID int64, managerUserID int64) (*Quotation, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.quotations SET assigned_manager_user_id = $2, updated_at = CURRENT_TIMESTAMP WHERE quotation_id = $1 AND deleted_at IS NULL`,
		quotationID, managerUserID,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, quotationID)
}

func (r *PostgresRepository) MarkConverted(ctx context.Context, quotationID int64, orderID int64, byUserID int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.quotations
		SET converted_order_id = $2, converted_by_user_id = $3, converted_at = CURRENT_TIMESTAMP,
		    status = 'CONVERTED', updated_at = CURRENT_TIMESTAMP
		WHERE quotation_id = $1 AND converted_order_id IS NULL AND deleted_at IS NULL
	`, quotationID, orderID, byUserID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND owner_user_id = $2)`,
		nurseryID, userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) GetNurseryInfo(ctx context.Context, nurseryID int64) (string, string, error) {
	var name string
	var phone sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT nursery_name, COALESCE(mobile, '') FROM public.nurseries WHERE nursery_id = $1`,
		nurseryID,
	).Scan(&name, &phone)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return name, phone.String, err
}

func (r *PostgresRepository) GetUserName(ctx context.Context, userID int64) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(first_name, mobile) FROM public.users WHERE user_id = $1`,
		userID,
	).Scan(&name)
	return name, err
}

func (r *PostgresRepository) GetUserMobile(ctx context.Context, userID int64) (string, error) {
	var mobile string
	err := r.db.QueryRowContext(ctx,
		`SELECT mobile FROM public.users WHERE user_id = $1`,
		userID,
	).Scan(&mobile)
	return mobile, err
}

func (r *PostgresRepository) GetPlantInfo(ctx context.Context, plantID int64) (string, string, error) {
	var scientific string
	var common sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT scientific_name, common_name FROM public.plants WHERE plant_id = $1`,
		plantID,
	).Scan(&scientific, &common)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return scientific, common.String, err
}

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM public.nursery_users
			WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true
			UNION ALL
			SELECT 1 FROM public.nurseries
			WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text, '') <> 'DELETED'
		)`,
		nurseryID, userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, input CreateAuditInput) error {
	const query = `
		INSERT INTO public.audit_logs (
			table_name, record_id, action_type, old_data, new_data, changed_by, source_ip, user_agent, changed_at
		)
		VALUES ($1,$2,$3,NULL,NULLIF($4,'')::jsonb,$5,NULLIF($6,''),NULLIF($7,''),$8)
	`
	_, err := r.db.ExecContext(ctx, query,
		input.TableName, input.RecordID, input.Action, input.NewJSON,
		input.ChangedBy, input.SourceIP, input.UserAgent, input.At,
	)
	return err
}

func (r *PostgresRepository) Approve(ctx context.Context, id int64, byUserID int64) (*Quotation, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.quotations
		SET status = 'CUSTOMER_SENT',
		    valid_until = CURRENT_TIMESTAMP + INTERVAL '15 days',
		    updated_at = CURRENT_TIMESTAMP
		WHERE quotation_id = $1 AND status = 'CUSTOMER_DRAFT' AND deleted_at IS NULL
	`, id)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) Recall(ctx context.Context, id int64) (*Quotation, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.quotations
		SET status = 'CUSTOMER_DRAFT', updated_at = CURRENT_TIMESTAMP
		WHERE quotation_id = $1 AND status = 'CUSTOMER_SENT' AND deleted_at IS NULL
	`, id)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) BuyerAccept(ctx context.Context, id int64, byUserID int64) (*Quotation, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.quotations
		SET status = 'CUSTOMER_ACCEPTED', updated_at = CURRENT_TIMESTAMP
		WHERE quotation_id = $1 AND status IN ('CUSTOMER_SENT') AND deleted_at IS NULL
	`, id)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) BuyerReject(ctx context.Context, id int64, byUserID int64, reason string) (*Quotation, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.quotations
		SET status = 'CUSTOMER_REJECTED', notes = CASE WHEN $2 <> '' THEN COALESCE(notes || E'\n', '') || $2 ELSE notes END, updated_at = CURRENT_TIMESTAMP
		WHERE quotation_id = $1 AND status = 'CUSTOMER_SENT' AND deleted_at IS NULL
	`, id, reason)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) GetBuyerNurseryID(ctx context.Context, quotationID int64) (*int64, error) {
	var id sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT buyer_nursery_id FROM public.quotations WHERE quotation_id = $1 AND deleted_at IS NULL`,
		quotationID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return nullableInt64(id), nil
}

func (r *PostgresRepository) GetOwnedNurseryID(ctx context.Context, userID int64) (*int64, error) {
	var id sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT nursery_id FROM public.nurseries WHERE owner_user_id = $1 AND COALESCE(status::text,'') <> 'DELETED' LIMIT 1`,
		userID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nullableInt64(id), nil
}

func (r *PostgresRepository) GetManagerNurseryID(ctx context.Context, userID int64) (*int64, error) {
	var id sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT nursery_id FROM public.nursery_users WHERE user_id = $1 AND COALESCE(is_active, true) = true LIMIT 1`,
		userID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nullableInt64(id), nil
}

func (r *PostgresRepository) IsNurseryActive(ctx context.Context, nurseryID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND status = 'ACTIVE')`,
		nurseryID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) GetOrderNurseryID(ctx context.Context, orderID int64) (*int64, error) {
	var id sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT nursery_id FROM public.orders WHERE order_id = $1`,
		orderID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return nullableInt64(id), nil
}

func (r *PostgresRepository) listItems(ctx context.Context, qID int64) ([]QuotationItem, error) {
	rows, err := r.db.QueryContext(ctx, itemSelect()+" WHERE qi.quotation_id = $1 ORDER BY qi.quotation_item_id", qID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]QuotationItem, 0)
	for rows.Next() {
		item, err := scanItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) createItemTx(ctx context.Context, tx *sql.Tx, qID int64, input QuotationItemRequest) error {
	scientific, common, err := r.GetPlantInfo(ctx, input.PlantID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("plant %d not found: %w", input.PlantID, ErrPlantNotFound)
		}
		return err
	}
	const query = `
		INSERT INTO public.quotation_items (
			quotation_id, plant_id, scientific_name, common_name, description, quantity, unit_price, total_price, created_at
		)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,CURRENT_TIMESTAMP)
	`
	_, err = tx.ExecContext(ctx, query,
		qID, input.PlantID, scientific, common, stringOrEmpty(input.Description),
		input.Quantity, input.UnitPrice, input.TotalPrice,
	)
	return err
}

func (r *PostgresRepository) refreshTotalTx(ctx context.Context, tx *sql.Tx, qID int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE public.quotations
		SET total_amount = COALESCE((SELECT SUM(total_price) FROM public.quotation_items WHERE quotation_id = $1), 0),
		    updated_at = CURRENT_TIMESTAMP
		WHERE quotation_id = $1
	`, qID)
	return err
}

// ── SQL helpers ───────────────────────────────────────────────────────────────

func baseSelect() string {
	return `
		SELECT q.quotation_id, q.quotation_code, q.quotation_type,
		       q.created_by_user_id, q.created_by_name,
		       q.nursery_id, q.nursery_name, q.nursery_phone,
		       q.customer_user_id, q.assigned_manager_user_id, q.converted_order_id,
		       q.recipient_name, q.recipient_mobile, q.notes,
		       COALESCE(q.total_amount, 0), q.status, q.valid_until, q.deleted_at, q.created_at, q.updated_at,
		       q.buyer_nursery_id,
		       NULLIF(TRIM(COALESCE(um.first_name, '') || ' ' || COALESCE(um.last_name, '')), '') AS assigned_manager_name
		FROM public.quotations q
		LEFT JOIN public.users um ON um.user_id = q.assigned_manager_user_id
		WHERE q.deleted_at IS NULL
	`
}

func baseCount() string {
	return `SELECT COUNT(*) FROM public.quotations q WHERE q.deleted_at IS NULL`
}

func itemSelect() string {
	return `
		SELECT qi.quotation_item_id, qi.quotation_id, qi.plant_id,
		       qi.scientific_name, qi.common_name, qi.description,
		       qi.quantity, qi.unit_price, qi.total_price, qi.created_at
		FROM public.quotation_items qi
	`
}

func buildWhere(input ListQuotationsRequest) (string, []any) {
	clauses := make([]string, 0)
	args := make([]any, 0)

	if input.Buying {
		// Buyer perspective: quotations where this user (or their nursery) is the buyer/recipient.
		// Matches both linked accounts (customer_user_id) and unlinked mobile-only quotations.
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
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("q.status = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(q.quotation_code ILIKE $%d OR q.created_by_name ILIKE $%d OR q.recipient_name ILIKE $%d OR q.nursery_name ILIKE $%d)", len(args), len(args), len(args), len(args)))
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
		createdByName         sql.NullString
		nurseryID             sql.NullInt64
		nurseryName           sql.NullString
		nurseryPhone          sql.NullString
		customerUserID        sql.NullInt64
		assignedManagerUserID sql.NullInt64
		convertedOrderID      sql.NullInt64
		recipientName         sql.NullString
		recipientMobile       sql.NullString
		notes                 sql.NullString
		validUntil            sql.NullTime
		deletedAt             sql.NullTime
		buyerNurseryID        sql.NullInt64
		totalAmount           sql.NullString
		assignedManagerName   sql.NullString
	)
	if err := row.Scan(
		&q.ID, &q.QuotationCode, &q.QuotationType,
		&q.CreatedByUserID, &createdByName,
		&nurseryID, &nurseryName, &nurseryPhone,
		&customerUserID, &assignedManagerUserID, &convertedOrderID,
		&recipientName, &recipientMobile, &notes,
		&totalAmount, &q.Status, &validUntil, &deletedAt, &q.CreatedAt, &q.UpdatedAt,
		&buyerNurseryID, &assignedManagerName,
	); err != nil {
		return Quotation{}, err
	}
	if totalAmount.Valid && totalAmount.String != "" {
		q.TotalAmount, _ = strconv.ParseFloat(totalAmount.String, 64)
	}
	q.CreatedByName = nullableString(createdByName)
	q.NurseryID = nullableInt64(nurseryID)
	q.NurseryName = nullableString(nurseryName)
	q.NurseryPhone = nullableString(nurseryPhone)
	q.CustomerUserID = nullableInt64(customerUserID)
	q.BuyerNurseryID = nullableInt64(buyerNurseryID)
	q.AssignedManagerUserID = nullableInt64(assignedManagerUserID)
	q.AssignedManagerName = nullableString(assignedManagerName)
	q.ConvertedOrderID = nullableInt64(convertedOrderID)
	q.RecipientName = nullableString(recipientName)
	q.RecipientMobile = nullableString(recipientMobile)
	q.Notes = nullableString(notes)
	if validUntil.Valid {
		q.ValidUntil = &validUntil.Time
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
	); err != nil {
		return QuotationItem{}, err
	}
	if quantity.Valid && quantity.String != "" {
		item.Quantity, _ = strconv.ParseFloat(quantity.String, 64)
	}
	if unitPrice.Valid && unitPrice.String != "" {
		item.UnitPrice, _ = strconv.ParseFloat(unitPrice.String, 64)
	}
	if totalPrice.Valid && totalPrice.String != "" {
		item.TotalPrice, _ = strconv.ParseFloat(totalPrice.String, 64)
	}
	item.CommonName = nullableString(commonName)
	item.Description = nullableString(description)
	return item, nil
}

// ── null helpers ──────────────────────────────────────────────────────────────

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

func validateItemMath(item QuotationItemRequest) bool {
	expected := item.Quantity * item.UnitPrice
	return math.Abs(expected-item.TotalPrice) <= 0.01
}
