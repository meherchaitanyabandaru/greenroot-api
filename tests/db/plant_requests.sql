-- Plant Requests data integrity checks
-- Run: psql 'postgres:///greenroot?host=/tmp' -f tests/db/plant_requests.sql

\echo '=== Plant Requests integrity ==='

\echo '-- Requests with no requesting user (should be 0)'
SELECT COUNT(*) AS no_requester
FROM plant_requests
WHERE requested_by_user_id IS NULL;

\echo '-- Responses with no parent request (should be 0)'
SELECT COUNT(*) AS orphaned_responses
FROM plant_request_responses prr
LEFT JOIN plant_requests pr ON prr.request_id = pr.request_id
WHERE pr.request_id IS NULL;

\echo '-- Status distribution'
SELECT status, COUNT(*) AS cnt FROM plant_requests GROUP BY status ORDER BY cnt DESC;

\echo '-- Requests with responses (informational)'
SELECT COUNT(DISTINCT request_id) AS requests_with_responses FROM plant_request_responses;

\echo '-- OPEN requests past expiry (should be closed by system or at read time)'
SELECT COUNT(*) AS open_past_expiry
FROM plant_requests
WHERE status = 'OPEN' AND expires_at < NOW();
