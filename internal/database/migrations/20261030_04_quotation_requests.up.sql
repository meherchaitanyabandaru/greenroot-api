-- Customer-initiated quotation requests (2026-10-30), inside the existing
-- quotation lifecycle:
--   CUSTOMER_REQUESTED (customer lists plants and quantities, unpriced)
--     → nursery prices the lines → send → CUSTOMER_SENT → accept/reject → CONVERTED
--     ↘ REQUEST_DECLINED (nursery cannot supply) ↘ REQUEST_CANCELLED (customer)
-- request_expires_at: the nursery has until then to send a first quotation.
-- request_fingerprint: hash of the nursery and lines; one open identical
-- request per customer (a double tap or a retry never creates two).
ALTER TABLE public.quotations ADD COLUMN IF NOT EXISTS requested_by_user_id bigint REFERENCES public.users(user_id) ON DELETE SET NULL;
ALTER TABLE public.quotations ADD COLUMN IF NOT EXISTS request_note text;
ALTER TABLE public.quotations ADD COLUMN IF NOT EXISTS request_expires_at timestamp with time zone;
ALTER TABLE public.quotations ADD COLUMN IF NOT EXISTS request_fingerprint character varying(64);
ALTER TABLE public.quotations ADD COLUMN IF NOT EXISTS decline_reason text;
ALTER TABLE public.quotations ADD COLUMN IF NOT EXISTS declined_at timestamp with time zone;
ALTER TABLE public.quotations ADD COLUMN IF NOT EXISTS cancelled_at timestamp with time zone;

ALTER TABLE public.quotations DROP CONSTRAINT IF EXISTS ck_quotations_status;
ALTER TABLE public.quotations ADD CONSTRAINT ck_quotations_status CHECK (status IN (
    'INTERNAL_DRAFT', 'CUSTOMER_DRAFT', 'CUSTOMER_SENT', 'CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED', 'EXPIRED',
    'CUSTOMER_REQUESTED', 'REQUEST_DECLINED', 'REQUEST_CANCELLED')) NOT VALID;
ALTER TABLE public.quotations VALIDATE CONSTRAINT ck_quotations_status;

CREATE UNIQUE INDEX IF NOT EXISTS uq_quotations_open_request
    ON public.quotations (requested_by_user_id, request_fingerprint)
    WHERE status = 'CUSTOMER_REQUESTED' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quotations_requested_by
    ON public.quotations (requested_by_user_id, created_at DESC) WHERE requested_by_user_id IS NOT NULL;
COMMENT ON COLUMN public.quotations.requested_by_user_id IS 'The customer who asked for this quotation (NULL: the nursery started it)';
COMMENT ON COLUMN public.quotations.request_note IS 'The customer''s message with the request (visible to both sides; notes stays nursery-internal)';
