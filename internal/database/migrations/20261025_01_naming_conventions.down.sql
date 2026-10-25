ALTER TABLE public.nurseries RENAME COLUMN market_blocked_until_at TO market_blocked_until;
ALTER TABLE public.market_enquiry_messages RENAME CONSTRAINT ck_market_enquiry_messages_status TO ck_market_enquiry_messages_offer_status;
ALTER TABLE public.market_enquiry_messages RENAME COLUMN status TO offer_status;
ALTER TABLE public.market_ads RENAME CONSTRAINT ck_market_ads_needed_by_date TO ck_market_ads_needed_by;
ALTER TABLE public.market_ads RENAME COLUMN needed_by_date TO needed_by;
