-- =============================================================================
-- Migration 2026-10-17 002: one row per gateway payment
-- =============================================================================
-- A retried or duplicated gateway callback must not record the same payment
-- twice. Offline payments (cash, cheque) have no provider_payment_id; the
-- repository stores '' as NULL, and NULLs are excluded here.
-- The API maps a violation to 409 duplicate_provider_payment.
--
-- CONCURRENTLY builds without blocking writes, so:
-- migrate:no-transaction
-- =============================================================================

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_payments_provider_payment
    ON public.payments USING btree (provider, provider_payment_id)
    WHERE provider_payment_id IS NOT NULL;
