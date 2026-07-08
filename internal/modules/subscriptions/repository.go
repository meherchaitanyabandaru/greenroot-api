package subscriptions

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
	ListPlans(ctx context.Context, activeOnly bool) ([]SubscriptionPlan, error)
	FindPlan(ctx context.Context, planID int64) (*SubscriptionPlan, error)
	FindPlanByCode(ctx context.Context, code string) (*SubscriptionPlan, error)
	List(ctx context.Context, input ListSubscriptionsRequest) ([]UserSubscription, int64, error)
	FindByID(ctx context.Context, subscriptionID int64) (*UserSubscription, error)
	FindActiveByUser(ctx context.Context, userID int64) (*UserSubscription, error)
	Create(ctx context.Context, input CreateSubscriptionInput) (*UserSubscription, error)
	UpdateStatus(ctx context.Context, subscriptionID int64, status string) (*UserSubscription, error)
	Renew(ctx context.Context, subscriptionID int64, input RenewSubscriptionInput) (*UserSubscription, error)
	CreatePayment(ctx context.Context, input CreatePaymentInput) error
	ListPaymentsBySubscription(ctx context.Context, subscriptionID int64) ([]Payment, error)
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
}

type CreateSubscriptionInput struct {
	UserID    int64
	PlanID    int64
	StartDate time.Time
	EndDate   time.Time
	AutoRenew bool
}

type RenewSubscriptionInput struct {
	StartDate time.Time
	EndDate   time.Time
	AutoRenew bool
}

type CreatePaymentInput struct {
	SubscriptionID  int64
	PayerUserID     int64
	Amount          float64
	Method          string
	Status          string
	Provider        *string
	ProviderOrderID *string
	Notes           string
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

func (r *PostgresRepository) ListPlans(ctx context.Context, activeOnly bool) ([]SubscriptionPlan, error) {
	query := planSelect()
	if activeOnly {
		query += " WHERE COALESCE(is_active, true) = true"
	}
	query += " ORDER BY monthly_price NULLS LAST, plan_id"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := make([]SubscriptionPlan, 0)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *plan)
	}
	return plans, rows.Err()
}

func (r *PostgresRepository) FindPlan(ctx context.Context, planID int64) (*SubscriptionPlan, error) {
	plan, err := scanPlan(r.db.QueryRowContext(ctx, planSelect()+" WHERE plan_id = $1", planID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return plan, err
}

func (r *PostgresRepository) FindPlanByCode(ctx context.Context, code string) (*SubscriptionPlan, error) {
	plan, err := scanPlan(r.db.QueryRowContext(ctx, planSelect()+" WHERE UPPER(plan_code) = UPPER($1)", code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return plan, err
}

func (r *PostgresRepository) List(ctx context.Context, input ListSubscriptionsRequest) ([]UserSubscription, int64, error) {
	where, args := buildSubscriptionWhere(input)
	var total int64
	if err := r.db.QueryRowContext(ctx, baseSubscriptionCount()+" "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(baseSubscriptionSelect()+`
		%s
		ORDER BY us.user_subscription_id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	subscriptions := make([]UserSubscription, 0)
	for rows.Next() {
		subscription, err := scanSubscription(rows)
		if err != nil {
			return nil, 0, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, total, rows.Err()
}

func (r *PostgresRepository) FindByID(ctx context.Context, subscriptionID int64) (*UserSubscription, error) {
	subscription, err := scanSubscription(r.db.QueryRowContext(ctx, baseSubscriptionSelect()+" WHERE us.user_subscription_id = $1", subscriptionID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &subscription, err
}

func (r *PostgresRepository) FindActiveByUser(ctx context.Context, userID int64) (*UserSubscription, error) {
	subscription, err := scanSubscription(r.db.QueryRowContext(ctx, baseSubscriptionSelect()+`
		WHERE us.user_id = $1
		  AND us.subscription_status::text = 'ACTIVE'
		  AND (us.end_date IS NULL OR us.end_date >= CURRENT_DATE)
		ORDER BY us.end_date DESC NULLS FIRST, us.user_subscription_id DESC
		LIMIT 1
	`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &subscription, err
}

func (r *PostgresRepository) Create(ctx context.Context, input CreateSubscriptionInput) (*UserSubscription, error) {
	subscriptionCode, err := publiccode.Next(ctx, r.db, publiccode.UserSubscriptions, time.Now())
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.user_subscriptions (
			subscription_code, user_id, plan_id, start_date, end_date, subscription_status, auto_renew, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'ACTIVE', $6, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING user_subscription_id
	`
	var subscriptionID int64
	if err := r.db.QueryRowContext(ctx, query, subscriptionCode, input.UserID, input.PlanID, input.StartDate, input.EndDate, input.AutoRenew).Scan(&subscriptionID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, subscriptionID)
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, subscriptionID int64, status string) (*UserSubscription, error) {
	const query = `
		UPDATE public.user_subscriptions
		SET subscription_status = $2,
			auto_renew = CASE WHEN $3 IN ('CANCELLED', 'EXPIRED') THEN false ELSE auto_renew END,
			updated_at = CURRENT_TIMESTAMP
		WHERE user_subscription_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, subscriptionID, status, status)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, subscriptionID)
}

func (r *PostgresRepository) Renew(ctx context.Context, subscriptionID int64, input RenewSubscriptionInput) (*UserSubscription, error) {
	const query = `
		UPDATE public.user_subscriptions
		SET start_date = $2,
			end_date = $3,
			subscription_status = 'ACTIVE',
			auto_renew = $4,
			updated_at = CURRENT_TIMESTAMP
		WHERE user_subscription_id = $1
	`
	result, err := r.db.ExecContext(ctx, query, subscriptionID, input.StartDate, input.EndDate, input.AutoRenew)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, subscriptionID)
}

func (r *PostgresRepository) CreatePayment(ctx context.Context, input CreatePaymentInput) error {
	if input.Amount <= 0 {
		return nil
	}
	now := time.Now()
	paymentCode, err := publiccode.Next(ctx, r.db, publiccode.Payments, now)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO public.payments (
			payment_code, payment_for, user_subscription_id, payer_user_id, amount, payment_method, payment_status,
			payment_date, notes, provider, provider_order_id, created_at, updated_at
		)
		VALUES ($1, 'SUBSCRIPTION', $2, $3, $4, $5, $6::text,
			CASE WHEN $6::text = 'SUCCESS' THEN $10::timestamp ELSE NULL::timestamp END,
			NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10, $10)
	`
	_, err = r.db.ExecContext(ctx, query, paymentCode, input.SubscriptionID, input.PayerUserID, input.Amount, input.Method, input.Status, input.Notes, stringOrEmpty(input.Provider), stringOrEmpty(input.ProviderOrderID), now)
	return err
}

func (r *PostgresRepository) ListPaymentsBySubscription(ctx context.Context, subscriptionID int64) ([]Payment, error) {
	const query = `
		SELECT payment_id, payment_code, amount, payment_method, transaction_reference,
		       payment_status, payment_date, provider, provider_payment_id, provider_order_id, created_at
		FROM public.payments
		WHERE user_subscription_id = $1 AND payment_for = 'SUBSCRIPTION'
		ORDER BY payment_id DESC
	`
	rows, err := r.db.QueryContext(ctx, query, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	payments := make([]Payment, 0)
	for rows.Next() {
		var p Payment
		var method, txRef, provider, providerPaymentID, providerOrderID sql.NullString
		var paymentDate sql.NullTime
		if err := rows.Scan(&p.ID, &p.PaymentCode, &p.Amount, &method, &txRef, &p.Status, &paymentDate, &provider, &providerPaymentID, &providerOrderID, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.PaymentMethod = nullableString(method)
		p.TransactionReference = nullableString(txRef)
		p.Provider = nullableString(provider)
		p.ProviderPaymentID = nullableString(providerPaymentID)
		p.ProviderOrderID = nullableString(providerOrderID)
		if paymentDate.Valid {
			p.PaymentDate = &paymentDate.Time
		}
		payments = append(payments, p)
	}
	return payments, rows.Err()
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

func planSelect() string {
	return `
		SELECT plan_id, plan_code, plan_name, description, monthly_price, yearly_price,
			max_users, max_nurseries, COALESCE(is_active, true), created_at, updated_at
		FROM public.subscription_plans
	`
}

func baseSubscriptionSelect() string {
	return `
		SELECT us.user_subscription_id, us.subscription_code, us.user_id, us.plan_id, sp.plan_code, sp.plan_name,
			us.start_date, us.end_date, us.subscription_status::text, COALESCE(us.auto_renew, false),
			us.created_at, us.updated_at,
			lp.payment_id, lp.payment_code, lp.amount, lp.payment_method, lp.transaction_reference,
			lp.payment_status, lp.payment_date, lp.provider, lp.provider_payment_id,
			lp.provider_order_id, lp.created_at
		FROM public.user_subscriptions us
		JOIN public.subscription_plans sp ON sp.plan_id = us.plan_id
		LEFT JOIN LATERAL (
			SELECT payment_id, payment_code, amount, payment_method, transaction_reference, payment_status,
				payment_date, provider, provider_payment_id, provider_order_id, created_at
			FROM public.payments
			WHERE user_subscription_id = us.user_subscription_id
			  AND payment_for = 'SUBSCRIPTION'
			ORDER BY payment_id DESC
			LIMIT 1
		) lp ON true
	`
}

func baseSubscriptionCount() string {
	return "SELECT COUNT(*) FROM public.user_subscriptions us JOIN public.subscription_plans sp ON sp.plan_id = us.plan_id"
}

func buildSubscriptionWhere(input ListSubscriptionsRequest) (string, []any) {
	clauses := []string{"1 = 1"}
	args := make([]any, 0)
	if input.UserID > 0 {
		args = append(args, input.UserID)
		clauses = append(clauses, fmt.Sprintf("us.user_id = $%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, input.Status)
		clauses = append(clauses, fmt.Sprintf("us.subscription_status::text = $%d", len(args)))
	}
	if input.Search != "" {
		args = append(args, "%"+input.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(us.subscription_code ILIKE $%d OR sp.plan_code ILIKE $%d OR sp.plan_name ILIKE $%d)", len(args), len(args), len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func scanPlan(row interface{ Scan(dest ...any) error }) (*SubscriptionPlan, error) {
	var plan SubscriptionPlan
	var description sql.NullString
	var monthlyPrice, yearlyPrice sql.NullFloat64
	var maxUsers, maxNurseries sql.NullInt64
	var updatedAt sql.NullTime
	if err := row.Scan(&plan.ID, &plan.Code, &plan.Name, &description, &monthlyPrice, &yearlyPrice, &maxUsers, &maxNurseries, &plan.IsActive, &plan.CreatedAt, &updatedAt); err != nil {
		return nil, err
	}
	plan.Description = nullableString(description)
	plan.MonthlyPrice = nullableFloat64(monthlyPrice)
	plan.YearlyPrice = nullableFloat64(yearlyPrice)
	plan.MaxUsers = nullableInt(maxUsers)
	plan.MaxNurseries = nullableInt(maxNurseries)
	if updatedAt.Valid {
		plan.UpdatedAt = &updatedAt.Time
	}
	return &plan, nil
}

func scanSubscription(row interface{ Scan(dest ...any) error }) (UserSubscription, error) {
	var subscription UserSubscription
	var endDate, updatedAt sql.NullTime
	var paymentID sql.NullInt64
	var paymentAmount sql.NullFloat64
	var paymentCode, paymentMethod, txRef, paymentStatus, provider, providerPaymentID, providerOrderID sql.NullString
	var paymentDate, paymentCreatedAt sql.NullTime
	if err := row.Scan(
		&subscription.ID,
		&subscription.SubscriptionCode,
		&subscription.UserID,
		&subscription.PlanID,
		&subscription.PlanCode,
		&subscription.PlanName,
		&subscription.StartDate,
		&endDate,
		&subscription.Status,
		&subscription.AutoRenew,
		&subscription.CreatedAt,
		&updatedAt,
		&paymentID,
		&paymentCode,
		&paymentAmount,
		&paymentMethod,
		&txRef,
		&paymentStatus,
		&paymentDate,
		&provider,
		&providerPaymentID,
		&providerOrderID,
		&paymentCreatedAt,
	); err != nil {
		return UserSubscription{}, err
	}
	if endDate.Valid {
		subscription.EndDate = &endDate.Time
		days := int(time.Until(endDate.Time).Hours() / 24)
		if days < 0 {
			days = 0
		}
		subscription.DaysRemaining = &days
	}
	if updatedAt.Valid {
		subscription.UpdatedAt = &updatedAt.Time
	}
	if paymentID.Valid {
		payment := Payment{
			ID:                   paymentID.Int64,
			PaymentCode:          paymentCode.String,
			Amount:               paymentAmount.Float64,
			PaymentMethod:        nullableString(paymentMethod),
			TransactionReference: nullableString(txRef),
			Status:               paymentStatus.String,
			Provider:             nullableString(provider),
			ProviderPaymentID:    nullableString(providerPaymentID),
			ProviderOrderID:      nullableString(providerOrderID),
			CreatedAt:            paymentCreatedAt.Time,
		}
		if paymentDate.Valid {
			payment.PaymentDate = &paymentDate.Time
		}
		subscription.LatestPayment = &payment
	}
	return subscription, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
