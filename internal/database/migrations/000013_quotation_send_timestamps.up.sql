-- 000013_quotation_send_timestamps.up.sql
-- Track the customer-visible send moment and first customer response moment.

ALTER TABLE public.quotations
    ADD COLUMN IF NOT EXISTS sent_at TIMESTAMP,
    ADD COLUMN IF NOT EXISTS customer_responded_at TIMESTAMP;

UPDATE public.quotations
SET sent_at = COALESCE(sent_at, updated_at, created_at)
WHERE status IN ('CUSTOMER_SENT', 'CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED')
  AND sent_at IS NULL;

UPDATE public.quotations
SET customer_responded_at = COALESCE(customer_responded_at, updated_at)
WHERE status IN ('CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED')
  AND customer_responded_at IS NULL;
