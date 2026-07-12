-- ============================================================
-- 000016 — Location Foundation
-- Reusable PostGIS-backed location support for addresses,
-- nursery defaults, market pickup points, tracking, and future
-- order delivery snapshots.
-- ============================================================

CREATE EXTENSION IF NOT EXISTS postgis;

-- ── User delivery addresses ─────────────────────────────────
ALTER TABLE public.user_addresses
    ADD COLUMN IF NOT EXISTS location GEOGRAPHY(POINT, 4326),
    ADD COLUMN IF NOT EXISTS gps_accuracy_meters NUMERIC(8,2),
    ADD COLUMN IF NOT EXISTS landmark VARCHAR(255),
    ADD COLUMN IF NOT EXISTS location_source VARCHAR(40),
    ADD COLUMN IF NOT EXISTS location_confirmed_by BIGINT,
    ADD COLUMN IF NOT EXISTS location_confirmed_at TIMESTAMP;

UPDATE public.user_addresses
SET location = ST_SetSRID(ST_MakePoint(longitude::double precision, latitude::double precision), 4326)::geography
WHERE latitude IS NOT NULL
  AND longitude IS NOT NULL
  AND location IS NULL;

ALTER TABLE public.user_addresses
    ADD CONSTRAINT chk_user_addresses_latitude
        CHECK (latitude IS NULL OR (latitude >= -90 AND latitude <= 90)),
    ADD CONSTRAINT chk_user_addresses_longitude
        CHECK (longitude IS NULL OR (longitude >= -180 AND longitude <= 180)),
    ADD CONSTRAINT chk_user_addresses_location_source
        CHECK (
            location_source IS NULL OR location_source IN (
                'gps_confirmed',
                'nursery_default',
                'map_selected',
                'address_search',
                'admin_updated'
            )
        );

ALTER TABLE public.user_addresses
    ADD CONSTRAINT user_addresses_location_confirmed_by_fkey
        FOREIGN KEY (location_confirmed_by) REFERENCES public.users(user_id);

CREATE INDEX IF NOT EXISTS idx_user_addresses_location
    ON public.user_addresses USING GIST (location);

-- ── Nursery addresses / default nursery location ─────────────
ALTER TABLE public.nursery_addresses
    ADD COLUMN IF NOT EXISTS location GEOGRAPHY(POINT, 4326),
    ADD COLUMN IF NOT EXISTS gps_accuracy_meters NUMERIC(8,2),
    ADD COLUMN IF NOT EXISTS landmark VARCHAR(255),
    ADD COLUMN IF NOT EXISTS location_source VARCHAR(40),
    ADD COLUMN IF NOT EXISTS location_confirmed_by BIGINT,
    ADD COLUMN IF NOT EXISTS location_confirmed_at TIMESTAMP;

UPDATE public.nursery_addresses
SET location = ST_SetSRID(ST_MakePoint(longitude::double precision, latitude::double precision), 4326)::geography
WHERE latitude IS NOT NULL
  AND longitude IS NOT NULL
  AND location IS NULL;

ALTER TABLE public.nursery_addresses
    ADD CONSTRAINT chk_nursery_addresses_latitude
        CHECK (latitude IS NULL OR (latitude >= -90 AND latitude <= 90)),
    ADD CONSTRAINT chk_nursery_addresses_longitude
        CHECK (longitude IS NULL OR (longitude >= -180 AND longitude <= 180)),
    ADD CONSTRAINT chk_nursery_addresses_location_source
        CHECK (
            location_source IS NULL OR location_source IN (
                'gps_confirmed',
                'nursery_default',
                'map_selected',
                'address_search',
                'admin_updated'
            )
        );

ALTER TABLE public.nursery_addresses
    ADD CONSTRAINT nursery_addresses_location_confirmed_by_fkey
        FOREIGN KEY (location_confirmed_by) REFERENCES public.users(user_id);

CREATE INDEX IF NOT EXISTS idx_nursery_addresses_location
    ON public.nursery_addresses USING GIST (location);

-- ── Market ad pickup location snapshot ───────────────────────
ALTER TABLE IF EXISTS public.market_ads
    ADD COLUMN IF NOT EXISTS pickup_address TEXT,
    ADD COLUMN IF NOT EXISTS pickup_landmark VARCHAR(255),
    ADD COLUMN IF NOT EXISTS pickup_latitude NUMERIC(10,7),
    ADD COLUMN IF NOT EXISTS pickup_longitude NUMERIC(10,7),
    ADD COLUMN IF NOT EXISTS pickup_location GEOGRAPHY(POINT, 4326),
    ADD COLUMN IF NOT EXISTS pickup_gps_accuracy_meters NUMERIC(8,2),
    ADD COLUMN IF NOT EXISTS pickup_location_source VARCHAR(40),
    ADD COLUMN IF NOT EXISTS pickup_confirmed_by BIGINT,
    ADD COLUMN IF NOT EXISTS pickup_confirmed_at TIMESTAMP;

ALTER TABLE IF EXISTS public.market_ads
    ADD CONSTRAINT chk_market_ads_pickup_latitude
        CHECK (pickup_latitude IS NULL OR (pickup_latitude >= -90 AND pickup_latitude <= 90)),
    ADD CONSTRAINT chk_market_ads_pickup_longitude
        CHECK (pickup_longitude IS NULL OR (pickup_longitude >= -180 AND pickup_longitude <= 180)),
    ADD CONSTRAINT chk_market_ads_pickup_location_source
        CHECK (
            pickup_location_source IS NULL OR pickup_location_source IN (
                'gps_confirmed',
                'nursery_default',
                'map_selected',
                'address_search',
                'admin_updated'
            )
        );

ALTER TABLE IF EXISTS public.market_ads
    ADD CONSTRAINT market_ads_pickup_confirmed_by_fkey
        FOREIGN KEY (pickup_confirmed_by) REFERENCES public.users(user_id);

DO $$
BEGIN
    IF to_regclass('public.market_ads') IS NOT NULL THEN
        CREATE INDEX IF NOT EXISTS idx_market_ads_pickup_location_published
            ON public.market_ads USING GIST (pickup_location)
            WHERE status = 'PUBLISHED';
    END IF;
END $$;

-- ── Vehicle tracking spatial index ───────────────────────────
ALTER TABLE public.vehicle_tracking
    ADD COLUMN IF NOT EXISTS location GEOGRAPHY(POINT, 4326);

UPDATE public.vehicle_tracking
SET location = ST_SetSRID(ST_MakePoint(longitude::double precision, latitude::double precision), 4326)::geography
WHERE latitude IS NOT NULL
  AND longitude IS NOT NULL
  AND location IS NULL;

CREATE INDEX IF NOT EXISTS idx_vehicle_tracking_location
    ON public.vehicle_tracking USING GIST (location);
