-- 000010_quotations_polish.up.sql
BEGIN;

-- Migrate any rows that used status='DELETED' to use soft-delete instead.
-- Status='DELETED' is redundant with deleted_at IS NOT NULL.
UPDATE public.quotations
    SET deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP)
    WHERE status = 'DELETED';

UPDATE public.quotations
    SET status = 'CONVERTED'
    WHERE status = 'DELETED' AND converted_order_id IS NOT NULL;

UPDATE public.quotations
    SET status = 'CUSTOMER_REJECTED'
    WHERE status = 'DELETED' AND converted_order_id IS NULL;

-- Now safe to tighten the CHECK constraint.
ALTER TABLE public.quotations DROP CONSTRAINT chk_quotation_status;
ALTER TABLE public.quotations ADD CONSTRAINT chk_quotation_status
    CHECK (status IN (
        'INTERNAL_DRAFT', 'CUSTOMER_DRAFT', 'CUSTOMER_SENT',
        'CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED'
    ));

-- Partial index for buyer-scoped list queries (buying=true, customer_user_id = ?)
CREATE INDEX idx_quotations_customer_user_id
    ON public.quotations (customer_user_id)
    WHERE deleted_at IS NULL AND customer_user_id IS NOT NULL;

COMMIT;
