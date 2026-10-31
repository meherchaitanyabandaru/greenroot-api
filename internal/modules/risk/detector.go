package risk

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// recentlyFlagged keeps a finding from being raised again for 30 days after
// it was flagged, whatever the admin decided.
const recentlyFlagged = `NOT EXISTS (SELECT 1 FROM public.risk_signals s WHERE s.signal_code = $3::varchar
	AND s.entity_type = c.entity_type AND s.entity_id = c.entity_id AND s.created_at > CURRENT_TIMESTAMP - INTERVAL '30 days')`

// checks: signal code → candidates (entity_type, entity_id, details) found
// with $1 = the rule's threshold. $2 (level) and $3 (code) are used by insert.
var checks = map[string]string{
	"HIGH_AMOUNT": `
		SELECT 'ORDER' AS entity_type, o.order_id AS entity_id, jsonb_build_object('total_amount', o.total_amount, 'threshold', $1::numeric) AS details
		FROM public.orders o
		WHERE o.created_at > CURRENT_TIMESTAMP - INTERVAL '2 days' AND o.status <> 'CANCELLED' AND o.total_amount > $1::numeric`,
	"PRICE_OUTLIER": `
		SELECT DISTINCT ON (oi.order_id) 'ORDER', oi.order_id,
			jsonb_build_object('plant_id', oi.plant_id, 'unit_price', oi.unit_price, 'usual_price', m.median, 'samples', m.n)
		FROM public.order_items oi
		JOIN public.orders o ON o.order_id = oi.order_id
		JOIN LATERAL (
			SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY x.unit_price) AS median, COUNT(*) AS n
			FROM public.order_items x JOIN public.orders xo ON xo.order_id = x.order_id
			WHERE x.plant_id = oi.plant_id AND x.order_id <> oi.order_id AND x.unit_price > 0
			  AND xo.created_at > CURRENT_TIMESTAMP - INTERVAL '90 days'
		) m ON m.n >= 5 AND m.median > 0
		WHERE o.created_at > CURRENT_TIMESTAMP - INTERVAL '2 days' AND oi.plant_id IS NOT NULL AND oi.unit_price > 0
		  AND (oi.unit_price > m.median * $1::numeric OR oi.unit_price < m.median / $1::numeric)
		ORDER BY oi.order_id, abs(ln(oi.unit_price / m.median)) DESC`,
	"DUPLICATE_ORDER": `
		SELECT 'ORDER', b.order_id, jsonb_build_object('same_as_order_id', a.order_id, 'total_amount', b.total_amount)
		FROM public.orders b
		JOIN public.orders a ON a.buyer_user_id = b.buyer_user_id AND a.seller_nursery_id = b.seller_nursery_id
			AND a.total_amount = b.total_amount AND a.order_id < b.order_id
			AND b.created_at - a.created_at <= make_interval(mins => $1::int)
		WHERE b.created_at > CURRENT_TIMESTAMP - INTERVAL '2 days' AND a.status <> 'CANCELLED' AND b.status <> 'CANCELLED'`,
	"REPEATED_CANCELLATIONS": `
		SELECT 'USER', o.buyer_user_id, jsonb_build_object('cancelled_orders', COUNT(*), 'days', 7)
		FROM public.orders o
		WHERE o.status = 'CANCELLED' AND o.cancelled_at > CURRENT_TIMESTAMP - INTERVAL '7 days' AND o.cancelled_by_user_id = o.buyer_user_id
		GROUP BY o.buyer_user_id HAVING COUNT(*) >= $1::numeric
		UNION ALL
		SELECT 'NURSERY', o.seller_nursery_id, jsonb_build_object('cancelled_orders', COUNT(*), 'days', 7)
		FROM public.orders o
		WHERE o.status = 'CANCELLED' AND o.cancelled_at > CURRENT_TIMESTAMP - INTERVAL '7 days'
		  AND o.cancelled_by_user_id IS DISTINCT FROM o.buyer_user_id AND o.seller_nursery_id IS NOT NULL
		GROUP BY o.seller_nursery_id HAVING COUNT(*) >= $1::numeric`,
	"QUANTITY_SHORTFALL": `
		SELECT 'ORDER', o.order_id, jsonb_build_object('loaded_percent', round(100 * SUM(COALESCE(oi.loaded_quantity, oi.quantity)) / SUM(oi.quantity), 1))
		FROM public.orders o JOIN public.order_items oi ON oi.order_id = o.order_id
		WHERE o.status IN ('PARTIALLY_FULFILLED', 'COMPLETED') AND o.updated_at > CURRENT_TIMESTAMP - INTERVAL '7 days'
		GROUP BY o.order_id
		HAVING 100 * SUM(COALESCE(oi.loaded_quantity, oi.quantity)) / SUM(oi.quantity) < $1::numeric`,
	"DELIVERY_DISPUTE": `
		SELECT 'ORDER', d.order_id, jsonb_build_object('dispatch_id', d.dispatch_id, 'issue', d.delivery_issue)
		FROM public.dispatches d
		WHERE d.delivery_issue IS NOT NULL AND d.updated_at > CURRENT_TIMESTAMP - INTERVAL '7 days' AND $1::numeric > 0`,
	"PAYMENT_MISMATCH": `
		SELECT 'ORDER', o.order_id, jsonb_build_object('paid', SUM(p.amount), 'total_amount', o.total_amount)
		FROM public.orders o JOIN public.payments p ON p.order_id = o.order_id AND p.status = 'SUCCESS'
		WHERE o.updated_at > CURRENT_TIMESTAMP - INTERVAL '30 days'
		GROUP BY o.order_id, o.total_amount
		HAVING SUM(p.amount) > o.total_amount * (1 + $1::numeric / 100)`,
	"DRIVER_DISPUTES": `
		SELECT 'DRIVER', d.driver_user_id, jsonb_build_object('disputed_trips', COUNT(*), 'days', 30)
		FROM public.dispatches d
		WHERE d.delivery_issue IS NOT NULL AND d.driver_user_id IS NOT NULL AND d.updated_at > CURRENT_TIMESTAMP - INTERVAL '30 days'
		GROUP BY d.driver_user_id HAVING COUNT(*) >= $1::numeric`,
}

type rule struct {
	code, level string
	threshold   float64
}

func enabledRules(ctx context.Context, db *sql.DB) ([]rule, error) {
	rows, err := db.QueryContext(ctx, `SELECT signal_code, threshold, risk_level FROM public.risk_rules WHERE is_enabled`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []rule
	for rows.Next() {
		var r rule
		if err := rows.Scan(&r.code, &r.threshold, &r.level); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// Scan runs every enabled rule once and returns how many signals it raised.
func Scan(ctx context.Context, db *sql.DB) (int, error) {
	rules, err := enabledRules(ctx, db)
	if err != nil {
		return 0, err
	}
	raised := 0
	for _, r := range rules {
		q, ok := checks[r.code]
		if !ok {
			continue
		}
		// #nosec G202 -- q and recentlyFlagged are fixed SQL; values are bound.
		res, err := db.ExecContext(ctx, `
			INSERT INTO public.risk_signals (signal_code, entity_type, entity_id, risk_level, details)
			SELECT $3::varchar, c.entity_type, c.entity_id, $2::varchar, c.details
			FROM (`+q+`) AS c (entity_type, entity_id, details)
			WHERE c.entity_id IS NOT NULL AND `+recentlyFlagged+`
			ON CONFLICT DO NOTHING`, r.threshold, r.level, r.code)
		if err != nil {
			slog.Error("risk rule failed", "rule", r.code, "error", err)
			continue
		}
		n, _ := res.RowsAffected()
		raised += int(n)
	}
	return raised, nil
}

// StartDetector scans every interval until ctx ends.
func StartDetector(ctx context.Context, db *sql.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if n, err := Scan(ctx, db); err != nil {
			slog.Error("risk scan failed", "error", err)
		} else if n > 0 {
			slog.Info("risk signals raised", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
