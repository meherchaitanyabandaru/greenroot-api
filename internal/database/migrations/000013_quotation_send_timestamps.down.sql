-- 000013_quotation_send_timestamps.down.sql

ALTER TABLE public.quotations
    DROP COLUMN IF EXISTS customer_responded_at,
    DROP COLUMN IF EXISTS sent_at;
