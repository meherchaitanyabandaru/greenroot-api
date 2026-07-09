-- Dispatches data integrity checks
-- Run: psql 'postgres:///greenroot?host=/tmp' -f tests/db/dispatches.sql

\echo '=== Dispatches integrity ==='

\echo '-- Dispatches with no order (should be 0)'
SELECT COUNT(*) AS orphaned_dispatches
FROM dispatches d
LEFT JOIN orders o ON d.order_id = o.order_id
WHERE o.order_id IS NULL;

\echo '-- Dispatches assigned to non-existent driver (should be 0)'
SELECT COUNT(*) AS bad_driver_ref
FROM dispatches d
LEFT JOIN drivers dr ON d.driver_id = dr.driver_id
WHERE d.driver_id IS NOT NULL AND dr.driver_id IS NULL;

\echo '-- Dispatches with DELIVERED status but order not COMPLETED (informational)'
SELECT COUNT(*) AS delivered_not_complete
FROM dispatches d
JOIN orders o ON d.order_id = o.order_id
WHERE d.status = 'DELIVERED' AND o.status != 'COMPLETED';

\echo '-- Status distribution'
SELECT status, COUNT(*) AS cnt FROM dispatches GROUP BY status ORDER BY cnt DESC;

\echo '-- Dispatches with tracking events (informational)'
SELECT COUNT(DISTINCT dispatch_id) AS dispatches_with_tracking FROM tracking;

\echo '-- Dispatches without tracking_uuid (should be 0 — all dispatches get one)'
SELECT COUNT(*) AS missing_uuid FROM dispatches WHERE tracking_uuid IS NULL;
