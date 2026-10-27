-- 18% GST is added on top of every subscription charge (2026-10-27).
-- payments.amount stays what was charged; gst_amount is the tax inside it
-- (NULL for payments taken before GST, and for order payments).
ALTER TABLE public.payments ADD COLUMN IF NOT EXISTS gst_amount numeric(15,2);
ALTER TABLE public.payments DROP CONSTRAINT IF EXISTS ck_payments_gst_amount;
ALTER TABLE public.payments ADD CONSTRAINT ck_payments_gst_amount
  CHECK (gst_amount IS NULL OR (gst_amount >= 0 AND gst_amount <= amount));
COMMENT ON COLUMN public.payments.gst_amount IS 'GST included in amount (subscriptions, 18%); NULL before GST was charged';
