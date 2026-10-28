-- Why the seller closed an ad (2026-10-28): its chats go read-only and both
-- sides see the reason. NULL for ads archived before this, and for other states.
ALTER TABLE public.market_ads ADD COLUMN IF NOT EXISTS close_reason varchar(30);
ALTER TABLE public.market_ads DROP CONSTRAINT IF EXISTS ck_market_ads_close_reason;
ALTER TABLE public.market_ads ADD CONSTRAINT ck_market_ads_close_reason
  CHECK (close_reason IS NULL OR close_reason IN ('SOLD_OUT', 'NO_LONGER_AVAILABLE'));
COMMENT ON COLUMN public.market_ads.close_reason IS 'Set when the seller closes (archives) the ad: SOLD_OUT or NO_LONGER_AVAILABLE';
