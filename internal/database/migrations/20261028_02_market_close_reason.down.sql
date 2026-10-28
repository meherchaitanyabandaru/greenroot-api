ALTER TABLE public.market_ads DROP CONSTRAINT IF EXISTS ck_market_ads_close_reason;
ALTER TABLE public.market_ads DROP COLUMN IF EXISTS close_reason;
