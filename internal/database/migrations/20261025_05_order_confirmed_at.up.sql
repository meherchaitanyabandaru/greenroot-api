-- When the nursery accepted (confirmed) the order: shown on the order
-- timeline. Set once, the first time the order becomes CONFIRMED; orders
-- confirmed before this column existed keep NULL (no time to show).
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS confirmed_at timestamp with time zone;
COMMENT ON COLUMN public.orders.confirmed_at IS 'First time the order became CONFIRMED (accepted by the nursery)';
