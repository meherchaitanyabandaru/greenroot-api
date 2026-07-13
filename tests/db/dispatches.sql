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

\echo '-- Dispatches with TRIP_STARTED status but order not in progress (informational)'
SELECT COUNT(*) AS active_trips_no_complete
FROM dispatches d
JOIN orders o ON d.order_id = o.order_id
WHERE d.dispatch_status = 'TRIP_STARTED' AND o.order_status = 'COMPLETED';

\echo '-- Status distribution'
SELECT dispatch_status, COUNT(*) AS cnt FROM dispatches GROUP BY dispatch_status ORDER BY cnt DESC;

\echo '-- Dispatches with tracking links (informational)'
SELECT COUNT(DISTINCT dispatch_id) AS dispatches_with_tracking FROM trip_tracking_links;

\echo '-- Dispatches without a tracking link (informational)'
SELECT COUNT(*) AS missing_tracking_link
FROM dispatches d
LEFT JOIN trip_tracking_links ttl ON ttl.dispatch_id = d.dispatch_id
WHERE ttl.id IS NULL;
