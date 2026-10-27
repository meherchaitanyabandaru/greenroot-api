ALTER TABLE public.payments DROP CONSTRAINT IF EXISTS ck_payments_gst_amount;
ALTER TABLE public.payments DROP COLUMN IF EXISTS gst_amount;
