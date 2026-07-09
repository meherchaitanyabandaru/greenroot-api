-- Subscriptions data integrity checks
-- Run: psql 'postgres:///greenroot?host=/tmp' -f tests/db/subscriptions.sql

\echo '=== Subscriptions integrity ==='

\echo '-- Subscriptions with no nursery (should be 0)'
SELECT COUNT(*) AS orphaned_subs
FROM subscriptions s
LEFT JOIN nurseries n ON s.nursery_id = n.nursery_id
WHERE n.nursery_id IS NULL;

\echo '-- Subscriptions with end_date before start_date (should be 0)'
SELECT COUNT(*) AS inverted_dates
FROM subscriptions
WHERE end_date < start_date;

\echo '-- Active subscriptions per nursery (more than 1 is unusual)'
SELECT nursery_id, COUNT(*) AS active_count
FROM subscriptions
WHERE status IN ('TRIAL', 'ACTIVE')
GROUP BY nursery_id
HAVING COUNT(*) > 1;

\echo '-- Status distribution'
SELECT status, COUNT(*) AS cnt FROM subscriptions GROUP BY status ORDER BY cnt DESC;

\echo '-- Plan type distribution'
SELECT sp.plan_type, COUNT(s.subscription_id) AS subscriptions
FROM subscription_plans sp
LEFT JOIN subscriptions s ON s.plan_id = sp.plan_id
GROUP BY sp.plan_type;

\echo '-- Subscription plans (should have TRIAL and STANDARD)'
SELECT plan_id, plan_type, name, is_active FROM subscription_plans ORDER BY plan_id;

\echo '-- Active promo codes'
SELECT promo_code, name, discount_type, discount_value, valid_until
FROM subscription_promos
WHERE is_active = true AND valid_until >= CURRENT_DATE
ORDER BY valid_until;
