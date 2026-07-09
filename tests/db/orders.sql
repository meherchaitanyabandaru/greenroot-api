-- Orders data integrity checks
-- Run: psql 'postgres:///greenroot?host=/tmp' -f tests/db/orders.sql

\echo '=== Orders integrity ==='

\echo '-- Orders with no nursery (should be 0)'
SELECT COUNT(*) AS orphaned_orders FROM orders WHERE nursery_id IS NULL;

\echo '-- Order items with no parent order (should be 0)'
SELECT COUNT(*) AS orphaned_items
FROM order_items oi
LEFT JOIN orders o ON oi.order_id = o.order_id
WHERE o.order_id IS NULL;

\echo '-- Orders with loaded_quantity > quantity on any item (should be 0)'
SELECT COUNT(*) AS overloaded_items
FROM order_items
WHERE loaded_quantity IS NOT NULL AND loaded_quantity > quantity;

\echo '-- Orders in LOADED/PARTIALLY_FULFILLED/COMPLETED with null total (should be 0)'
SELECT COUNT(*) AS bad_totals
FROM orders
WHERE status IN ('LOADED', 'PARTIALLY_FULFILLED', 'COMPLETED')
  AND total_amount IS NULL;

\echo '-- Status distribution'
SELECT status, COUNT(*) AS cnt FROM orders GROUP BY status ORDER BY cnt DESC;

\echo '-- COMPLETED orders without a dispatch (informational)'
SELECT COUNT(*) AS completed_no_dispatch
FROM orders o
LEFT JOIN dispatches d ON d.order_id = o.order_id
WHERE o.status = 'COMPLETED' AND d.dispatch_id IS NULL;
