package payments

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
	List(ctx context.Context, input ListPaymentsRequest) ([]Payment, int64, error)
	FindByID(ctx context.Context, paymentID int64) (*Payment, error)
	Create(ctx context.Context, input CreatePaymentInput) (*Payment, error)
	UpdateStatus(ctx context.Context, paymentID int64, input UpdatePaymentInput) (*Payment, error)
	OrderAccess(ctx context.Context, orderID int64) (*OrderAccess, error)
	SubscriptionAccess(ctx context.Context, subscriptionID int64) (*SubscriptionAccess, error)
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
}

type CreatePaymentInput struct {
	PaymentFor           string
	OrderID              *int64
	UserSubscriptionID   *int64
	PayerUserID          *int64
	Amount               float64
	Method               string
	TransactionReference *string
	Status               string
	Notes                *string
	Provider             *string
	ProviderPaymentID    *string
	ProviderOrderID      *string
	ProviderSignature    *string
	RawResponseJSON      string
}

type UpdatePaymentInput struct {
	Status               string
	TransactionReference *string
	Notes                *string
	Provider             *string
	ProviderPaymentID    *string
	ProviderOrderID      *string
	ProviderSignature    *string
	RawResponseJSON      string
}

type OrderAccess struct {
	OrderID   int64
	BuyerID   *int64
	NurseryID *int64
}

type SubscriptionAccess struct {
	SubscriptionID int64
	UserID         int64
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

func (r *PostgresRepository) List(ctx context.Context, input ListPaymentsRequest) ([]Payment, int64, error) {
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
	payments := make([]Payment, 0)
	for rows.Next() {
		payment, err := scanPaymentRows(rows)
		if err != nil {
			return nil, 0, err
		}
		payments = append(payments, payment)
	}
	return payments, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, paymentID int64) (*Payment, error) {
	payment, err := scanPaymentRow(r.db.QueryRowContext(ctx, baseSelect()+" WHERE p.payment_id = $1", paymentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return payment, err
}

func (r *PostgresRepository) Create(ctx context.Context, input CreatePaymentInput) (*Payment, error) {
	now := time.Now()
	paymentCode, err := publiccode.Next(ctx, r.db, publiccode.Payments, now)
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.payments (
			payment_code, payment_for, order_id, user_subscription_id, payer_user_id, amount, payment_method,
			transaction_reference, payment_status, payment_date, notes, provider, provider_payment_id,
			provider_order_id, provider_signature, raw_response, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9::text,
			CASE WHEN $9::text = 'SUCCESS' THEN $16::timestamp ELSE NULL::timestamp END,
			NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''),
			$15::jsonb, $16, $16)
		RETURNING payment_id
	`
	var paymentID int64
	if err := r.db.QueryRowContext(
		ctx,
		query,
		paymentCode,
		input.PaymentFor,
		int64OrNil(input.OrderID),
		int64OrNil(input.UserSubscriptionID),
		int64OrNil(input.PayerUserID),
		input.Amount,
		input.Method,
		stringOrEmpty(input.TransactionReference),
		input.Status,
		stringOrEmpty(input.Notes),
		stringOrEmpty(input.Provider),
		stringOrEmpty(input.ProviderPaymentID),
		stringOrEmpty(input.ProviderOrderID),
		stringOrEmpty(input.ProviderSignature),
		jsonOrNil(input.RawResponseJSON),
		now,
	).Scan(&paymentID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, paymentID)
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, paymentID int64, input UpdatePaymentInput) (*Payment, error) {
	const query = `
		UPDATE public.payments
		SET payment_status = $2,
			transaction_reference = COALESCE(NULLIF($3, ''), transaction_reference),
			payment_date = CASE WHEN $10 = 'SUCCESS' THEN COALESCE(payment_date, CURRENT_TIMESTAMP) ELSE payment_date END,
			notes = COALESCE(NULLIF($4, ''), notes),
			provider = COALESCE(NULLIF($5, ''), provider),
			provider_payment_id = COALESCE(NULLIF($6, ''), provider_payment_id),
			provider_order_id = COALESCE(NULLIF($7, ''), provider_order_id),
			provider_signature = COALESCE(NULLIF($8, ''), provider_signature),
			raw_response = COALESCE(NULLIF($9, '')::jsonb, raw_response),
			updated_at = CURRENT_TIMESTAMP
		WHERE payment_id = $1
	`
	result, err := r.db.ExecContext(
		ctx,
		query,
		paymentID,
		input.Status,
		stringOrEmpty(input.TransactionReference),
		stringOrEmpty(input.Notes),
		stringOrEmpty(input.Provider),
		stringOrEmpty(input.ProviderPaymentID),
		stringOrEmpty(input.ProviderOrderID),
		stringOrEmpty(input.ProviderSignature),
		input.RawResponseJSON,
		input.Status,
	)
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
	return r.FindByID(ctx, paymentID)
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

func (r *PostgresRepository) SubscriptionAccess(ctx context.Context, subscriptionID int64) (*SubscriptionAccess, error) {
	var access SubscriptionAccess
	err := r.db.QueryRowContext(ctx, `SELECT user_subscription_id, user_id FROM public.user_subscriptions WHERE user_subscription_id = $1`, subscriptionID).Scan(&access.SubscriptionID, &access.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &access, err
}

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.nursery_users WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true)`, nurseryID, userID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, input CreateAuditInput) error {
	const query = `
		INSERT INTO public.audit_logs (
			table_name, record_id, action_type, old_data, new_data, changed_by, source_ip, user_agent, changed_at
		)
		VALUES ($1, $2, $3, NULL, NULLIF($4, '')::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
	`
	_, err := r.db.ExecContext(ctx, query, input.TableName, input.RecordID, input.Action, input.NewJSON, input.ChangedBy, input.SourceIP, input.UserAgent, input.At)
	return err
}

func baseSelect() string {
	return `
		SELECT p.payment_id, p.payment_code, p.payment_for, p.order_id, o.order_number, p.user_subscription_id,
			sp.plan_name, p.payer_user_id, u.first_name, p.amount, p.payment_method::text,
			p.transaction_reference, p.payment_status::text, p.payment_date, p.notes,
			p.provider, p.provider_payment_id, p.provider_order_id, p.provider_signature,
			p.raw_response::text, p.created_at, p.updated_at
		FROM public.payments p
		LEFT JOIN public.orders o ON o.order_id = p.order_id
		LEFT JOIN public.user_subscriptions us ON us.user_subscription_id = p.user_subscription_id
		LEFT JOIN public.subscription_plans sp ON sp.plan_id = us.plan_id
		LEFT JOIN public.users u ON u.user_id = p.payer_user_id
	`
}

func baseCount() string {
	return `
		SELECT COUNT(*)
		FROM public.payments p
		LEFT JOIN public.orders o ON o.order_id = p.order_id
		LEFT JOIN public.user_subscriptions us ON us.user_subscription_id = p.user_subscription_id
		LEFT JOIN public.subscription_plans sp ON sp.plan_id = us.plan_id
		LEFT JOIN public.users u ON u.user_id = p.payer_user_id
	`
}

func buildWhere(input ListPaymentsRequest) (string, []any) {
	clauses := []string{"1 = 1"}
	args := make([]any, 0)
	if input.PaymentFor != "" {
		args = append(args, input.PaymentFor)
		clauses = append(clauses, fmt.Sprintf("p.payment_for = $%d", len(args)))
	}
	if input.OrderID > 0 {
		args = append(args, input.OrderID)
		clauses = append(clauses, fmt.Sprintf("p.order_id = $%d", len(args)))
	}
	if input.SubscriptionID > 0 {
		args = append(args, input.SubscriptionID)
		clauses = append(clauses, fmt.Sprintf("p.user_subscription_id = $%d", len(args)))
	}
	if input.PayerUserID > 0 {
		args = append(args, input.PayerUserID)
		clauses = append(clauses, fmt.Sprintf("p.payer_user_id = $%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("p.payment_status::text = $%d", len(args)))
	}
	if input.Method != "" {
		args = append(args, input.Method)
		clauses = append(clauses, fmt.Sprintf("p.payment_method::text = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(p.payment_code ILIKE $%d OR o.order_number ILIKE $%d OR sp.plan_name ILIKE $%d OR u.first_name ILIKE $%d OR p.transaction_reference ILIKE $%d OR p.provider_payment_id ILIKE $%d OR p.provider_order_id ILIKE $%d OR p.notes ILIKE $%d)", len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func sortClause(input ListPaymentsRequest) string {
	direction := "DESC"
	if strings.EqualFold(input.SortOrder, "asc") {
		direction = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(input.SortBy)) {
	case "id":
		return "p.payment_id " + direction
	case "payment_code":
		return "p.payment_code " + direction + " NULLS LAST, p.payment_id DESC"
	case "payment_for":
		return "p.payment_for " + direction + " NULLS LAST, p.payment_id DESC"
	case "order_number":
		return "o.order_number " + direction + " NULLS LAST, p.payment_id DESC"
	case "subscription_plan":
		return "sp.plan_name " + direction + " NULLS LAST, p.payment_id DESC"
	case "payer_name":
		return "u.first_name " + direction + " NULLS LAST, p.payment_id DESC"
	case "amount":
		return "p.amount " + direction + " NULLS LAST, p.payment_id DESC"
	case "payment_method":
		return "p.payment_method " + direction + " NULLS LAST, p.payment_id DESC"
	case "payment_status", "status":
		return "p.payment_status " + direction + " NULLS LAST, p.payment_id DESC"
	case "payment_date":
		return "p.payment_date " + direction + " NULLS LAST, p.payment_id DESC"
	case "created_at":
		return "p.created_at " + direction + " NULLS LAST, p.payment_id DESC"
	default:
		return "p.payment_id DESC"
	}
}

func scanPaymentRow(row interface{ Scan(dest ...any) error }) (*Payment, error) {
	payment, err := scanPayment(row)
	if err != nil {
		return nil, err
	}
	return &payment, nil
}

func scanPaymentRows(rows *sql.Rows) (Payment, error) {
	return scanPayment(rows)
}

func scanPayment(row interface{ Scan(dest ...any) error }) (Payment, error) {
	var payment Payment
	var orderID, subscriptionID, payerUserID sql.NullInt64
	var orderNumber, planName, payerName, method, txRef, notes, provider, providerPaymentID, providerOrderID, providerSignature, raw sql.NullString
	var paymentDate sql.NullTime
	if err := row.Scan(
		&payment.ID,
		&payment.PaymentCode,
		&payment.PaymentFor,
		&orderID,
		&orderNumber,
		&subscriptionID,
		&planName,
		&payerUserID,
		&payerName,
		&payment.Amount,
		&method,
		&txRef,
		&payment.Status,
		&paymentDate,
		&notes,
		&provider,
		&providerPaymentID,
		&providerOrderID,
		&providerSignature,
		&raw,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	); err != nil {
		return Payment{}, err
	}
	payment.OrderID = nullableInt64(orderID)
	payment.OrderNumber = nullableString(orderNumber)
	payment.UserSubscriptionID = nullableInt64(subscriptionID)
	payment.SubscriptionPlan = nullableString(planName)
	payment.PayerUserID = nullableInt64(payerUserID)
	payment.PayerName = nullableString(payerName)
	payment.PaymentMethod = nullableString(method)
	payment.TransactionReference = nullableString(txRef)
	if paymentDate.Valid {
		payment.PaymentDate = &paymentDate.Time
	}
	payment.Notes = nullableString(notes)
	payment.Provider = nullableString(provider)
	payment.ProviderPaymentID = nullableString(providerPaymentID)
	payment.ProviderOrderID = nullableString(providerOrderID)
	payment.ProviderSignature = nullableString(providerSignature)
	payment.RawResponse = nullableString(raw)
	return payment, nil
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

func jsonOrNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
