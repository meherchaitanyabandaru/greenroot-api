-- 000010_quotations_polish.down.sql
BEGIN;

DROP INDEX IF EXISTS public.idx_quotations_customer_user_id;

ALTER TABLE public.quotations DROP CONSTRAINT IF EXISTS chk_quotation_status;
ALTER TABLE public.quotations ADD CONSTRAINT chk_quotation_status
    CHECK (status IN (
        'INTERNAL_DRAFT', 'CUSTOMER_DRAFT', 'CUSTOMER_SENT',
        'CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED', 'DELETED'
    ));

COMMIT;
