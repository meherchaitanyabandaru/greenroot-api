-- 000008_quotations_hardening.down.sql
BEGIN;

ALTER TABLE public.quotations DROP COLUMN IF EXISTS valid_until;

DROP INDEX IF EXISTS public.idx_quotations_recipient_mobile;
DROP INDEX IF EXISTS public.idx_quotations_buyer_nursery;
DROP INDEX IF EXISTS public.idx_quotations_nursery;
DROP INDEX IF EXISTS public.idx_quotations_created_by;

CREATE INDEX IF NOT EXISTS idx_quotations_created_by  ON public.quotations (created_by_user_id);
CREATE INDEX IF NOT EXISTS idx_quotations_nursery_id  ON public.quotations (nursery_id);
CREATE INDEX IF NOT EXISTS idx_quotations_buyer_nursery ON public.quotations (buyer_nursery_id);

ALTER TABLE public.quotation_items
    DROP CONSTRAINT IF EXISTS fk_quot_items_quotation;
ALTER TABLE public.quotations
    DROP CONSTRAINT IF EXISTS fk_quot_order,
    DROP CONSTRAINT IF EXISTS fk_quot_manager,
    DROP CONSTRAINT IF EXISTS fk_quot_customer,
    DROP CONSTRAINT IF EXISTS fk_quot_nursery;

ALTER TABLE public.quotation_items
    ADD COLUMN IF NOT EXISTS plant_name_snapshot VARCHAR(255),
    ADD COLUMN IF NOT EXISTS size VARCHAR(100),
    ADD COLUMN IF NOT EXISTS remarks TEXT;

ALTER TABLE public.quotations
    ADD COLUMN IF NOT EXISTS customer_name   VARCHAR(255),
    ADD COLUMN IF NOT EXISTS customer_mobile VARCHAR(20);

ALTER TABLE public.quotations DROP CONSTRAINT IF EXISTS chk_quotation_status;
ALTER TABLE public.quotations ALTER COLUMN status SET DEFAULT 'DRAFT';

ALTER TABLE public.quotations DROP CONSTRAINT IF EXISTS chk_quotation_type;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS quotation_type;

COMMIT;
