package subscriptions

import (
	"context"
	"database/sql"
	"encoding/json"
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
	UpdatePlan(ctx context.Context, planID int64, input UpdatePlanInput) (*SubscriptionPlan, error)
	List(ctx context.Context, input ListSubscriptionsRequest) ([]UserSubscription, int64, error)
	FindByID(ctx context.Context, subscriptionID int64) (*UserSubscription, error)
	FindActiveByUser(ctx context.Context, userID int64) (*UserSubscription, error)
	Create(ctx context.Context, input CreateSubscriptionInput) (*UserSubscription, error)
	UpdateStatus(ctx context.Context, subscriptionID int64, status string) (*UserSubscription, error)
	Renew(ctx context.Context, subscriptionID int64, input RenewSubscriptionInput) (*UserSubscription, error)
	CreatePayment(ctx context.Context, input CreatePaymentInput) error
	ListPaymentsBySubscription(ctx context.Context, subscriptionID int64) ([]Payment, error)
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
	// Promos
	ListPromos(ctx context.Context, activeOnly bool) ([]SubscriptionPromo, error)
	FindPromo(ctx context.Context, promoID int64) (*SubscriptionPromo, error)
	FindPromoByCode(ctx context.Context, code string) (*SubscriptionPromo, error)
	CreatePromo(ctx context.Context, input CreatePromoInput) (*SubscriptionPromo, error)
	UpdatePromo(ctx context.Context, promoID int64, input UpdatePromoInput) (*SubscriptionPromo, error)
	IncrementPromoUsed(ctx context.Context, promoID int64) error
	FindUnsubscribedOwnerIDs(ctx context.Context) ([]int64, error)
	BulkCreateNotifications(ctx context.Context, inputs []BulkNotificationInput) (int, error)
}

type CreatePromoInput struct {
	PromoCode        string
	Name             string
	Description      *string
	DiscountType     string
	DiscountValue    float64
	MaxDiscountCap   *float64
	ApplicablePlans  []string
	ApplicableCycles []string
	ValidFrom        string
	ValidUntil       string
	MaxUses          *int
	CreatedBy        int64
}

type UpdatePromoInput struct {
	Name             string
	Description      *string
	DiscountType     string
	DiscountValue    float64
	MaxDiscountCap   *float64
	ApplicablePlans  []string
	ApplicableCycles []string
	ValidFrom        string
	ValidUntil       string
	IsActive         bool
	MaxUses          *int
}

type BulkNotificationInput struct {
	UserID  int64
	Type    string
	Title   string
	Message string
	DataJSON string
}

type UpdatePlanInput struct {
	Name         string
	Description  *string
	SixMonthPrice float64
	YearlyPrice  float64
	MaxManagers  *int
	IsActive     bool
	Features     map[string]any
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

func (r *PostgresRepository) UpdatePlan(ctx context.Context, planID int64, input UpdatePlanInput) (*SubscriptionPlan, error) {
	featuresJSON, err := json.Marshal(input.Features)
	if err != nil {
		return nil, err
	}
	var maxManagers *int64
	if input.MaxManagers != nil {
		v := int64(*input.MaxManagers)
		maxManagers = &v
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE public.subscription_plans SET
			plan_name = $1, description = $2, monthly_price = $3, yearly_price = $4,
			max_managers = $5, is_active = $6, features = $7, updated_at = NOW()
		WHERE plan_id = $8
	`, input.Name, input.Description, input.SixMonthPrice, input.YearlyPrice,
		maxManagers, input.IsActive, featuresJSON, planID)
	if err != nil {
		return nil, err
	}
	return r.FindPlan(ctx, planID)
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
			max_managers, max_nurseries, COALESCE(is_active, true), created_at, updated_at,
			COALESCE(features, '{}')::jsonb
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
	var maxManagers, maxNurseries sql.NullInt64
	var updatedAt sql.NullTime
	var featuresJSON []byte
	if err := row.Scan(
		&plan.ID, &plan.Code, &plan.Name, &description,
		&monthlyPrice, &yearlyPrice,
		&maxManagers, &maxNurseries,
		&plan.IsActive, &plan.CreatedAt, &updatedAt,
		&featuresJSON,
	); err != nil {
		return nil, err
	}
	plan.Description = nullableString(description)
	plan.MonthlyPrice = nullableFloat64(monthlyPrice)  // kept for cycleEndAndAmount
	plan.SixMonthPrice = nullableFloat64(monthlyPrice) // API-facing field
	plan.YearlyPrice = nullableFloat64(yearlyPrice)
	plan.MaxManagers = nullableInt(maxManagers)
	plan.MaxNurseries = nullableInt(maxNurseries)
	if updatedAt.Valid {
		plan.UpdatedAt = &updatedAt.Time
	}
	if len(featuresJSON) > 0 {
		_ = json.Unmarshal(featuresJSON, &plan.Features)
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

// ── Promo repository ──────────────────────────────────────────────────────────

func (r *PostgresRepository) ListPromos(ctx context.Context, activeOnly bool) ([]SubscriptionPromo, error) {
	query := promoSelect()
	if activeOnly {
		query += " WHERE p.is_active = true AND p.valid_until >= CURRENT_DATE"
	}
	query += " ORDER BY p.promo_id DESC"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	promos := make([]SubscriptionPromo, 0)
	for rows.Next() {
		promo, err := scanPromo(rows)
		if err != nil {
			return nil, err
		}
		promos = append(promos, *promo)
	}
	return promos, rows.Err()
}

func (r *PostgresRepository) FindPromo(ctx context.Context, promoID int64) (*SubscriptionPromo, error) {
	promo, err := scanPromo(r.db.QueryRowContext(ctx, promoSelect()+" WHERE p.promo_id = $1", promoID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return promo, err
}

func (r *PostgresRepository) FindPromoByCode(ctx context.Context, code string) (*SubscriptionPromo, error) {
	promo, err := scanPromo(r.db.QueryRowContext(ctx, promoSelect()+" WHERE UPPER(p.promo_code) = UPPER($1)", code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return promo, err
}

func (r *PostgresRepository) CreatePromo(ctx context.Context, input CreatePromoInput) (*SubscriptionPromo, error) {
	plansJSON, _ := json.Marshal(input.ApplicablePlans)
	cyclesJSON, _ := json.Marshal(input.ApplicableCycles)
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO public.subscription_promos
			(promo_code, name, description, discount_type, discount_value, max_discount_cap,
			 applicable_plans, applicable_cycles, valid_from, valid_until, max_uses, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::date, $10::date, $11, $12)
		RETURNING promo_id
	`, strings.ToUpper(strings.TrimSpace(input.PromoCode)), input.Name, input.Description,
		input.DiscountType, input.DiscountValue, input.MaxDiscountCap,
		plansJSON, cyclesJSON, input.ValidFrom, input.ValidUntil, input.MaxUses, input.CreatedBy,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.FindPromo(ctx, id)
}

func (r *PostgresRepository) UpdatePromo(ctx context.Context, promoID int64, input UpdatePromoInput) (*SubscriptionPromo, error) {
	plansJSON, _ := json.Marshal(input.ApplicablePlans)
	cyclesJSON, _ := json.Marshal(input.ApplicableCycles)
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.subscription_promos SET
			name = $1, description = $2, discount_type = $3, discount_value = $4, max_discount_cap = $5,
			applicable_plans = $6::jsonb, applicable_cycles = $7::jsonb,
			valid_from = $8::date, valid_until = $9::date, is_active = $10, max_uses = $11, updated_at = NOW()
		WHERE promo_id = $12
	`, input.Name, input.Description, input.DiscountType, input.DiscountValue, input.MaxDiscountCap,
		plansJSON, cyclesJSON, input.ValidFrom, input.ValidUntil, input.IsActive, input.MaxUses, promoID,
	)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindPromo(ctx, promoID)
}

func (r *PostgresRepository) IncrementPromoUsed(ctx context.Context, promoID int64) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE public.subscription_promos SET used_count = used_count + 1, updated_at = NOW() WHERE promo_id = $1",
		promoID)
	return err
}

func (r *PostgresRepository) FindUnsubscribedOwnerIDs(ctx context.Context) ([]int64, error) {
	const query = `
		SELECT DISTINCT ur.user_id
		FROM public.user_roles ur
		WHERE ur.role_id = 3  -- Nursery Owner
		  AND NOT EXISTS (
			SELECT 1 FROM public.user_subscriptions us
			WHERE us.user_id = ur.user_id
			  AND us.subscription_status = 'ACTIVE'
			  AND (us.end_date IS NULL OR us.end_date >= CURRENT_DATE)
		  )
	`
	rows, err := r.db.QueryContext(ctx, query)
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

func (r *PostgresRepository) BulkCreateNotifications(ctx context.Context, inputs []BulkNotificationInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO public.notifications (user_id, notification_type, title, message, channel, notification_status, data, sent_at, created_at)
		VALUES ($1, $2, $3, $4, 'IN_APP', 'SENT', $5::jsonb, NOW(), NOW())
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	count := 0
	for _, n := range inputs {
		if _, err := stmt.ExecContext(ctx, n.UserID, n.Type, n.Title, n.Message, n.DataJSON); err == nil {
			count++
		}
	}
	return count, tx.Commit()
}

func promoSelect() string {
	return `
		SELECT p.promo_id, p.promo_code, p.name, p.description,
			p.discount_type, p.discount_value, p.max_discount_cap,
			p.applicable_plans, p.applicable_cycles,
			p.valid_from, p.valid_until, p.is_active,
			p.max_uses, p.used_count, p.created_by,
			p.created_at, p.updated_at
		FROM public.subscription_promos p
	`
}

func scanPromo(row interface{ Scan(dest ...any) error }) (*SubscriptionPromo, error) {
	var p SubscriptionPromo
	var description sql.NullString
	var maxDiscountCap sql.NullFloat64
	var maxUses sql.NullInt64
	var createdBy sql.NullInt64
	var updatedAt sql.NullTime
	var plansJSON, cyclesJSON []byte
	var validFrom, validUntil time.Time

	if err := row.Scan(
		&p.ID, &p.PromoCode, &p.Name, &description,
		&p.DiscountType, &p.DiscountValue, &maxDiscountCap,
		&plansJSON, &cyclesJSON,
		&validFrom, &validUntil,
		&p.IsActive, &maxUses, &p.UsedCount, &createdBy,
		&p.CreatedAt, &updatedAt,
	); err != nil {
		return nil, err
	}

	p.Description = nullableString(description)
	p.ValidFrom = validFrom.Format("2006-01-02")
	p.ValidUntil = validUntil.Format("2006-01-02")
	if maxDiscountCap.Valid {
		p.MaxDiscountCap = &maxDiscountCap.Float64
	}
	if maxUses.Valid {
		v := int(maxUses.Int64)
		p.MaxUses = &v
	}
	if createdBy.Valid {
		p.CreatedBy = &createdBy.Int64
	}
	if updatedAt.Valid {
		p.UpdatedAt = &updatedAt.Time
	}
	if len(plansJSON) > 0 {
		_ = json.Unmarshal(plansJSON, &p.ApplicablePlans)
	}
	if len(cyclesJSON) > 0 {
		_ = json.Unmarshal(cyclesJSON, &p.ApplicableCycles)
	}
	if p.ApplicablePlans == nil {
		p.ApplicablePlans = []string{}
	}
	if p.ApplicableCycles == nil {
		p.ApplicableCycles = []string{}
	}
	return &p, nil
}
