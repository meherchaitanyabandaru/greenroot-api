-- Chat delivery receipts (WhatsApp-style ticks): when each side's app last
-- fetched its chats. A message is delivered once the other side's time
-- passes it, read once their *_read_at does.
ALTER TABLE public.market_enquiries ADD COLUMN IF NOT EXISTS ad_nursery_delivered_at timestamp with time zone;
ALTER TABLE public.market_enquiries ADD COLUMN IF NOT EXISTS enquirer_delivered_at timestamp with time zone;
COMMENT ON COLUMN public.market_enquiries.ad_nursery_delivered_at IS 'When the ad''s nursery last fetched its chats; the other side''s messages before this are delivered';
COMMENT ON COLUMN public.market_enquiries.enquirer_delivered_at IS 'When the enquiring nursery last fetched its chats';
