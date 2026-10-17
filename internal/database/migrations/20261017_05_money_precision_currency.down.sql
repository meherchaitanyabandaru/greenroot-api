-- Rollback for 20261017_05_money_precision_currency (manual; the runner only applies up).
-- Narrowing precision fails if any value no longer fits; check first.
ALTER TABLE public.subscription_plans DROP COLUMN IF EXISTS currency;
ALTER TABLE public.payments           DROP COLUMN IF EXISTS currency;
ALTER TABLE public.quotations         DROP COLUMN IF EXISTS currency;
ALTER TABLE public.orders             DROP COLUMN IF EXISTS currency;
ALTER TABLE public.market_ads          ALTER COLUMN price_per_unit TYPE numeric(12,2);
ALTER TABLE public.subscription_promos ALTER COLUMN discount_value TYPE numeric(10,2), ALTER COLUMN max_discount_cap TYPE numeric(10,2);
ALTER TABLE public.subscription_plans  ALTER COLUMN monthly_price TYPE numeric(12,2), ALTER COLUMN yearly_price TYPE numeric(12,2);
ALTER TABLE public.quotation_items     ALTER COLUMN unit_price TYPE numeric(12,2), ALTER COLUMN total_price TYPE numeric(12,2);
ALTER TABLE public.quotations          ALTER COLUMN total_amount TYPE numeric(12,2);
