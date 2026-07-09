-- Quotations data integrity checks
-- Run: psql 'postgres:///greenroot?host=/tmp' -f tests/db/quotations.sql

\echo '=== Quotations integrity ==='

\echo '-- Quotations with no nursery (should be 0)'
SELECT COUNT(*) AS orphaned_quotations
FROM quotations q
LEFT JOIN nurseries n ON q.nursery_id = n.nursery_id
WHERE n.nursery_id IS NULL;

\echo '-- CONVERTED quotations without a linked order (should be 0)'
SELECT COUNT(*) AS converted_no_order
FROM quotations
WHERE status = 'CONVERTED' AND order_id IS NULL;

\echo '-- Quotation items with no parent quotation (should be 0)'
SELECT COUNT(*) AS orphaned_items
FROM quotation_items qi
LEFT JOIN quotations q ON qi.quotation_id = q.quotation_id
WHERE q.quotation_id IS NULL;

\echo '-- Status distribution'
SELECT status, COUNT(*) AS cnt FROM quotations GROUP BY status ORDER BY cnt DESC;

\echo '-- Type distribution'
SELECT quotation_type, COUNT(*) AS cnt FROM quotations GROUP BY quotation_type ORDER BY cnt DESC;

\echo '-- Expired APPROVED quotations (may need status refresh at read time)'
SELECT COUNT(*) AS expired_approved
FROM quotations
WHERE status = 'APPROVED' AND expiry_date < NOW();
