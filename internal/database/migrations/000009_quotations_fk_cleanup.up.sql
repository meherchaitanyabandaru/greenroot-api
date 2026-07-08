-- 000009_quotations_fk_cleanup.up.sql
-- Drop duplicate FK constraints added by migration 000008 that already existed
-- under the original auto-generated names from the initial schema.
BEGIN;

ALTER TABLE public.quotations
    DROP CONSTRAINT IF EXISTS fk_quot_customer,
    DROP CONSTRAINT IF EXISTS fk_quot_manager,
    DROP CONSTRAINT IF EXISTS fk_quot_nursery,
    DROP CONSTRAINT IF EXISTS fk_quot_order;

-- Composite index to speed up buyer-perspective lookup by mobile + status
-- (e.g., WHERE recipient_mobile = ? AND status = 'CUSTOMER_SENT' AND deleted_at IS NULL)
DROP INDEX IF EXISTS public.idx_quotations_recipient_mobile;
CREATE INDEX idx_quotations_recipient_mobile
    ON public.quotations (recipient_mobile, status)
    WHERE deleted_at IS NULL AND recipient_mobile IS NOT NULL;

COMMIT;
