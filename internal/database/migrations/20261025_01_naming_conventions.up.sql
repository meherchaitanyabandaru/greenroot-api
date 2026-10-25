-- Bring three market columns in line with the naming standard (D13), which
-- TestSchemaNamingConvention enforces. JSON field names are unchanged.
ALTER TABLE public.market_ads RENAME COLUMN needed_by TO needed_by_date;
ALTER TABLE public.market_ads RENAME CONSTRAINT ck_market_ads_needed_by TO ck_market_ads_needed_by_date;

-- The table's only status column is called status.
ALTER TABLE public.market_enquiry_messages RENAME COLUMN offer_status TO status;
ALTER TABLE public.market_enquiry_messages RENAME CONSTRAINT ck_market_enquiry_messages_offer_status TO ck_market_enquiry_messages_status;
COMMENT ON COLUMN public.market_enquiry_messages.status IS 'Offers only: PENDING until the other nursery accepts or declines; WITHDRAWN when a newer offer replaces it';

ALTER TABLE public.nurseries RENAME COLUMN market_blocked_until TO market_blocked_until_at;
