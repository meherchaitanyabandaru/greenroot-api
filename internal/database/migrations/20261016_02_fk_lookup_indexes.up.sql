-- =============================================================================
-- Migration 2026-10-16 002: indexes for foreign keys used in read paths
-- =============================================================================
-- Each index backs a query the API runs today (found by reviewing all 75
-- unindexed foreign keys against the repository SQL); audit-only columns
-- (created_by, updated_by, ...) are deliberately left unindexed.
--
-- CONCURRENTLY builds without locking writes, so this is safe on a live
-- database; it cannot run in a transaction, hence:
-- migrate:no-transaction
-- =============================================================================

-- Address book: GET /users/{id}/addresses and every order/quotation create.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_addresses_user
    ON public.user_addresses USING btree (user_id);

-- Primary-address join on every order/quotation/sourcing read, plus
-- GET /nurseries/{id}/addresses.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_nursery_addresses_nursery
    ON public.nursery_addresses USING btree (nursery_id);

-- Manager quotation tabs ("Assigned to me": created_by OR assigned_manager)
-- and account-deletion checks.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_quotations_assigned_manager
    ON public.quotations USING btree (assigned_manager_user_id) WHERE (deleted_at IS NULL);

-- Account-deletion check (customer_user_id OR buyer_user_id); buyer side is
-- already indexed, so without this the OR falls back to a full scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_customer_user
    ON public.orders USING btree (customer_user_id);
