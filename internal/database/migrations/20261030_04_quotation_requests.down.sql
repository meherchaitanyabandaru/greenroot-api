-- Manual rollback. Requests must be removed (or moved to another status) first.
DROP INDEX IF EXISTS public.idx_quotations_requested_by;
DROP INDEX IF EXISTS public.uq_quotations_open_request;
ALTER TABLE public.quotations DROP CONSTRAINT IF EXISTS ck_quotations_status;
ALTER TABLE public.quotations ADD CONSTRAINT ck_quotations_status CHECK (status IN (
    'INTERNAL_DRAFT', 'CUSTOMER_DRAFT', 'CUSTOMER_SENT', 'CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED', 'EXPIRED'));
ALTER TABLE public.quotations DROP COLUMN IF EXISTS cancelled_at;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS declined_at;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS decline_reason;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS request_fingerprint;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS request_expires_at;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS request_note;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS requested_by_user_id;
