-- Rollback for 20261017_02_payment_provider_unique (manual; the runner only applies up).
DROP INDEX CONCURRENTLY IF EXISTS public.uq_payments_provider_payment;
