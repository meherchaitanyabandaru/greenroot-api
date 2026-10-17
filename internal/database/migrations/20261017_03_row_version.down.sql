-- Rollback for 20261017_03_row_version (manual; the runner only applies up).
DROP TRIGGER IF EXISTS trg_nursery_inventory_version ON public.nursery_inventory;
DROP TRIGGER IF EXISTS trg_dispatches_version ON public.dispatches;
DROP TRIGGER IF EXISTS trg_payments_version ON public.payments;
DROP TRIGGER IF EXISTS trg_quotations_version ON public.quotations;
DROP TRIGGER IF EXISTS trg_orders_version ON public.orders;
ALTER TABLE public.nursery_inventory DROP COLUMN IF EXISTS version;
ALTER TABLE public.dispatches DROP COLUMN IF EXISTS version;
ALTER TABLE public.payments DROP COLUMN IF EXISTS version;
ALTER TABLE public.quotations DROP COLUMN IF EXISTS version;
ALTER TABLE public.orders DROP COLUMN IF EXISTS version;
DROP FUNCTION IF EXISTS public.bump_row_version();
