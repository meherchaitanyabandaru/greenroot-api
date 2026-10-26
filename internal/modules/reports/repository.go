package reports

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) OwnedNurseryID(ctx context.Context, userID int64) (int64, error) {
	return r.oneID(ctx, `SELECT nursery_id FROM public.nurseries
		WHERE owner_user_id = $1 AND COALESCE(status::text, '') <> 'DELETED' LIMIT 1`, userID)
}

func (r *Repository) MemberNurseryID(ctx context.Context, userID int64) (int64, error) {
	return r.oneID(ctx, `SELECT nursery_id FROM public.nursery_users
		WHERE user_id = $1 AND COALESCE(is_active, true) = true ORDER BY nursery_id LIMIT 1`, userID)
}

func (r *Repository) oneID(ctx context.Context, q string, arg int64) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, q, arg).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// query is one report's FROM, WHERE (with the caller's scope and the date
// range) and the column the range applies to. Every value is a parameter.
type query struct {
	from, where, date string
	args              []any
}

func (q *query) arg(v any) string {
	q.args = append(q.args, v)
	return "$" + strconv.Itoa(len(q.args))
}

func build(req Request, sc Scope) query {
	var q query
	// The user id becomes a parameter only when the scope uses it.
	user := ""
	me := func() string {
		if user == "" {
			user = q.arg(sc.UserID)
		}
		return user
	}
	var nurseryCol, mgrA, mgrB, driverCol string
	var buyerCols []string
	var buyerMobile string
	switch req.Type {
	case TypeOrders:
		q.from = `public.orders o
			LEFT JOIN public.users cu ON cu.user_id = COALESCE(o.customer_user_id, o.buyer_user_id)
			LEFT JOIN public.nurseries pn ON pn.nursery_id = o.seller_nursery_id`
		q.where, q.date = "o.deleted_at IS NULL", "COALESCE(o.ordered_at, o.created_at)"
		nurseryCol, mgrA, mgrB = "o.seller_nursery_id", "o.assigned_manager_user_id", "o.created_by_user_id"
		buyerCols = []string{"o.buyer_user_id", "o.customer_user_id"}
	case TypeQuotations:
		q.from = `public.quotations x
			LEFT JOIN public.users cu ON cu.user_id = x.customer_user_id
			LEFT JOIN public.nurseries pn ON pn.nursery_id = x.nursery_id`
		// Drafts were never sent to anyone: not part of the business record.
		q.where = "x.deleted_at IS NULL AND x.status NOT IN ('INTERNAL_DRAFT', 'CUSTOMER_DRAFT')"
		q.date = "x.created_at"
		nurseryCol, mgrA, mgrB = "x.nursery_id", "x.assigned_manager_user_id", "x.created_by_user_id"
		buyerCols = []string{"x.customer_user_id"}
		// Same rule as the quotations list: also those sent to the user's
		// mobile and not yet linked to the account.
		buyerMobile = "x.recipient_mobile"
	case TypeDeliveries:
		q.from = `public.dispatches d
			LEFT JOIN public.orders do2 ON do2.order_id = d.order_id
			LEFT JOIN public.users cu ON cu.user_id = d.customer_user_id
			LEFT JOIN public.nurseries pn ON pn.nursery_id = d.nursery_id`
		q.where, q.date = "d.deleted_at IS NULL", "d.created_at"
		nurseryCol, mgrA, mgrB = "d.nursery_id", "d.assigned_manager_user_id", "d.dispatched_by_user_id"
		buyerCols, driverCol = []string{"d.customer_user_id"}, "d.driver_user_id"
	}

	switch sc.Kind {
	case ScopeAll:
		if sc.NurseryID > 0 {
			q.where += " AND " + nurseryCol + " = " + q.arg(sc.NurseryID)
		}
	case ScopeNursery:
		q.where += " AND " + nurseryCol + " = " + q.arg(sc.NurseryID)
	case ScopeManager:
		q.where += " AND " + nurseryCol + " = " + q.arg(sc.NurseryID) +
			" AND (" + mgrA + " = " + me() + " OR " + mgrB + " = " + me() + ")"
	case ScopeDriver:
		q.where += " AND " + driverCol + " = " + me()
	default: // ScopeBuyer
		conds := make([]string, len(buyerCols))
		for i, c := range buyerCols {
			conds[i] = c + " = " + me()
		}
		if buyerMobile != "" {
			conds = append(conds, buyerMobile+" = (SELECT mobile FROM public.users WHERE user_id = "+me()+")")
		}
		q.where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	q.where += " AND " + q.date + " >= " + q.arg(req.From) + " AND " + q.date + " < " + q.arg(req.ToExcl)
	return q
}

func (r *Repository) Summary(ctx context.Context, req Request, sc Scope) (Summary, error) {
	q := build(req, sc)
	var s Summary
	var err error
	switch req.Type {
	case TypeOrders:
		// Sales = delivered (COMPLETED) orders only; cancelled never count;
		// paid = successful payments (a refund turns the payment REFUNDED).
		err = r.db.QueryRowContext(ctx, `
			SELECT count(*),
				count(*) FILTER (WHERE o.status = 'COMPLETED'),
				count(*) FILTER (WHERE o.status = 'CANCELLED'),
				COALESCE(sum(o.total_amount) FILTER (WHERE o.status = 'COMPLETED'), 0),
				COALESCE(sum(o.total_amount) FILTER (WHERE o.status NOT IN ('COMPLETED', 'CANCELLED')), 0),
				COALESCE((SELECT sum(p.amount) FROM public.payments p
					WHERE p.payment_for = 'ORDER' AND p.status = 'SUCCESS'
					  AND p.order_id = ANY (array_agg(o.order_id))), 0)
			FROM `+q.from+` WHERE `+q.where, q.args...).
			Scan(&s.Total, &s.Delivered, &s.Cancelled, &s.Sales, &s.OpenValue, &s.PaidAmount)
	case TypeQuotations:
		err = r.db.QueryRowContext(ctx, `
			SELECT count(*),
				count(*) FILTER (WHERE x.status IN ('CUSTOMER_ACCEPTED', 'CONVERTED')),
				count(*) FILTER (WHERE x.status = 'CUSTOMER_SENT'),
				count(*) FILTER (WHERE x.status IN ('CUSTOMER_REJECTED', 'EXPIRED')),
				COALESCE(sum(x.total_amount) FILTER (WHERE x.status IN ('CUSTOMER_ACCEPTED', 'CONVERTED')), 0)
			FROM `+q.from+` WHERE `+q.where, q.args...).
			Scan(&s.Total, &s.Accepted, &s.Pending, &s.RejectedExpired, &s.AcceptedValue)
	case TypeDeliveries:
		err = r.db.QueryRowContext(ctx, `
			SELECT count(*),
				count(*) FILTER (WHERE d.status = 'DELIVERED'),
				count(*) FILTER (WHERE d.status IN ('PENDING', 'ACCEPTED', 'DISPATCHED', 'IN_TRANSIT')),
				count(*) FILTER (WHERE d.status = 'CANCELLED')
			FROM `+q.from+` WHERE `+q.where, q.args...).
			Scan(&s.Total, &s.Delivered, &s.InProgress, &s.Cancelled)
	}
	return s, err
}

// rowsSQL selects the report's rows, newest first. The "party" is the other
// side: the nursery when the caller is buying, else the customer.
func rowsSQL(req Request, sc Scope, q *query) string {
	buying := sc.Kind == ScopeBuyer
	day := "to_char(" + q.date + " AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD')"
	userName := "NULLIF(TRIM(COALESCE(cu.first_name, '') || ' ' || COALESCE(cu.last_name, '')), '')"
	var cols, order string
	switch req.Type {
	case TypeOrders:
		party := "COALESCE(NULLIF(o.customer_name, ''), " + userName + ", '')"
		mobile := "COALESCE(NULLIF(o.customer_mobile, ''), cu.mobile, '')"
		if buying {
			party, mobile = "COALESCE(pn.nursery_name, '')", "''"
		}
		cols = "o.order_code, " + day + ", " + party + ", " + mobile + ", o.status, o.total_amount, " +
			`(SELECT COALESCE(sum(p.amount), 0) FROM public.payments p
				WHERE p.payment_for = 'ORDER' AND p.status = 'SUCCESS' AND p.order_id = o.order_id), ''`
		order = q.date + " DESC, o.order_id DESC"
	case TypeQuotations:
		party := "COALESCE(NULLIF(x.recipient_name, ''), " + userName + ", '')"
		mobile := "COALESCE(NULLIF(x.recipient_mobile, ''), cu.mobile, '')"
		if buying {
			party, mobile = "COALESCE(pn.nursery_name, '')", "''"
		}
		cols = "x.quotation_code, " + day + ", " + party + ", " + mobile + ", x.status, x.total_amount, NULL::numeric, ''"
		order = q.date + " DESC, x.quotation_id DESC"
	case TypeDeliveries:
		party := "COALESCE(NULLIF(d.customer_name_snapshot, ''), NULLIF(do2.customer_name, ''), " + userName + ", '')"
		mobile := "COALESCE(NULLIF(d.customer_mobile_snapshot, ''), NULLIF(do2.customer_mobile, ''), cu.mobile, '')"
		if buying {
			party, mobile = "COALESCE(pn.nursery_name, '')", "''"
		}
		cols = "d.dispatch_code, " + day + ", " + party + ", " + mobile + ", d.status, NULL::numeric, NULL::numeric, COALESCE(do2.order_code, '')"
		order = q.date + " DESC, d.dispatch_id DESC"
	}
	return "SELECT " + cols + " FROM " + q.from + " WHERE " + q.where + " ORDER BY " + order
}

func scanRow(rows *sql.Rows) (Row, error) {
	var row Row
	var amount, paid sql.NullFloat64
	if err := rows.Scan(&row.Reference, &row.Date, &row.Party, &row.Mobile, &row.Status, &amount, &paid, &row.OrderRef); err != nil {
		return row, err
	}
	if row.Mobile != "" {
		row.Mobile = MaskMobile(row.Mobile)
	}
	if amount.Valid {
		row.Amount = &amount.Float64
	}
	if paid.Valid {
		row.Paid = &paid.Float64
	}
	return row, nil
}

func (r *Repository) Rows(ctx context.Context, req Request, sc Scope, limit, offset int) ([]Row, error) {
	q := build(req, sc)
	sqlText := rowsSQL(req, sc, &q) + " LIMIT " + q.arg(limit) + " OFFSET " + q.arg(offset)
	rows, err := r.db.QueryContext(ctx, sqlText, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// StreamCSV writes every row in the range, one at a time, so a three-year
// export never sits in memory.
func (r *Repository) StreamCSV(ctx context.Context, req Request, sc Scope, w io.Writer) error {
	q := build(req, sc)
	rows, err := r.db.QueryContext(ctx, rowsSQL(req, sc, &q), q.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	cw := csv.NewWriter(w)
	header := csvHeader(req.Type, sc.Kind == ScopeBuyer)
	if err := cw.Write(header); err != nil {
		return err
	}
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return err
		}
		if err := cw.Write(csvRecord(req.Type, sc.Kind == ScopeBuyer, row)); err != nil {
			return err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	return rows.Err()
}

func csvHeader(reportType string, buying bool) []string {
	party := "Customer"
	if buying {
		party = "Nursery"
	}
	h := []string{"Reference", "Date", party}
	if !buying {
		h = append(h, "Mobile")
	}
	h = append(h, "Status")
	switch reportType {
	case TypeOrders:
		h = append(h, "Amount (INR)", "Paid (INR)")
	case TypeQuotations:
		h = append(h, "Amount (INR)")
	case TypeDeliveries:
		h = append(h, "Order")
	}
	return h
}

func csvRecord(reportType string, buying bool, row Row) []string {
	rec := []string{csvSafe(row.Reference), row.Date, csvSafe(row.Party)}
	if !buying {
		rec = append(rec, csvSafe(row.Mobile))
	}
	rec = append(rec, row.Status)
	money := func(v *float64) string {
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%.2f", *v)
	}
	switch reportType {
	case TypeOrders:
		rec = append(rec, money(row.Amount), money(row.Paid))
	case TypeQuotations:
		rec = append(rec, money(row.Amount))
	case TypeDeliveries:
		rec = append(rec, csvSafe(row.OrderRef))
	}
	return rec
}
