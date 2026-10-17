-- Rollback for 20261017_06_drop_orders_nursery_id (manual; the runner only applies up).
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS nursery_id bigint REFERENCES public.nurseries(nursery_id);
