-- Subscriptions data integrity checks
-- Run: psql 'postgres:///greenroot?host=/tmp' -f tests/db/subscriptions.sql

\echo '=== Subscriptions integrity ==='

\echo '-- Subscriptions with no user (should be 0)'
SELECT COUNT(*) AS orphaned_subs
FROM user_subscriptions s
LEFT JOIN users u ON s.user_id = u.user_id
WHERE u.user_id IS NULL;

\echo '-- Subscriptions with end_date before start_date (should be 0)'
SELECT COUNT(*) AS inverted_dates
FROM user_subscriptions
WHERE end_date < start_date;

\echo '-- Active subscriptions per user (more than 1 is unusual)'
SELECT user_id, COUNT(*) AS active_count
FROM user_subscriptions
WHERE subscription_status IN ('TRIAL', 'ACTIVE')
GROUP BY user_id
HAVING COUNT(*) > 1;

\echo '-- Status distribution'
SELECT subscription_status, COUNT(*) AS cnt FROM user_subscriptions GROUP BY subscription_status ORDER BY cnt DESC;

\echo '-- Plan code distribution'
SELECT sp.plan_code, COUNT(s.user_subscription_id) AS subscriptions
FROM subscription_plans sp
LEFT JOIN user_subscriptions s ON s.plan_id = sp.plan_id
GROUP BY sp.plan_code
ORDER BY sp.plan_code;

\echo '-- Subscription plans (should have FREE and TRIAL at minimum)'
SELECT plan_id, plan_code, plan_name, is_active FROM subscription_plans ORDER BY plan_id;

\echo '-- Active promo codes'
SELECT promo_code, name, discount_type, discount_value, valid_until
FROM subscription_promos
WHERE is_active = true AND valid_until >= CURRENT_DATE
ORDER BY valid_until;
