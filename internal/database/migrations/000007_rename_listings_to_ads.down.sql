-- Reverse 000007 — restore original table/column names

ALTER TABLE public.market_enquiries RENAME COLUMN ad_nursery_id TO listing_nursery_id;
ALTER TABLE public.market_enquiries RENAME COLUMN ad_id         TO listing_id;

ALTER TABLE public.market_ad_reports RENAME COLUMN ad_id TO listing_id;
ALTER TABLE public.market_ad_views   RENAME COLUMN ad_id TO listing_id;
ALTER TABLE public.market_ad_saves   RENAME COLUMN ad_id TO listing_id;

ALTER TABLE public.market_ads RENAME COLUMN ad_code TO listing_code;
ALTER TABLE public.market_ads RENAME COLUMN ad_id   TO listing_id;

ALTER TABLE public.market_ad_reports RENAME TO market_listing_reports;
ALTER TABLE public.market_ad_views   RENAME TO market_listing_views;
ALTER TABLE public.market_ad_saves   RENAME TO market_listing_saves;
ALTER TABLE public.market_ads        RENAME TO market_listings;
