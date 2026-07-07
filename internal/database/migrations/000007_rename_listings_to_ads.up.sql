-- ============================================================
-- 000007 — Rename market_listings → market_ads
-- Uniform terminology: "ad" replaces "listing" across DB, API, UI.
-- The next_public_code sequence key stays 'market_listings' to
-- preserve existing MKT-XXXXXX codes without gaps.
-- ============================================================

-- Rename tables
ALTER TABLE public.market_listings       RENAME TO market_ads;
ALTER TABLE public.market_listing_saves  RENAME TO market_ad_saves;
ALTER TABLE public.market_listing_views  RENAME TO market_ad_views;
ALTER TABLE public.market_listing_reports RENAME TO market_ad_reports;

-- Rename columns in market_ads
ALTER TABLE public.market_ads RENAME COLUMN listing_id   TO ad_id;
ALTER TABLE public.market_ads RENAME COLUMN listing_code TO ad_code;

-- Rename FK column in market_ad_saves
ALTER TABLE public.market_ad_saves RENAME COLUMN listing_id TO ad_id;

-- Rename FK column in market_ad_views
ALTER TABLE public.market_ad_views RENAME COLUMN listing_id TO ad_id;

-- Rename FK column in market_ad_reports
ALTER TABLE public.market_ad_reports RENAME COLUMN listing_id TO ad_id;

-- Rename FK columns in market_enquiries
ALTER TABLE public.market_enquiries RENAME COLUMN listing_id        TO ad_id;
ALTER TABLE public.market_enquiries RENAME COLUMN listing_nursery_id TO ad_nursery_id;
