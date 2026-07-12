-- Reverse of 000016_location_foundation.up.sql.

DROP INDEX IF EXISTS public.idx_vehicle_tracking_location;
ALTER TABLE public.vehicle_tracking
    DROP COLUMN IF EXISTS location;

DROP INDEX IF EXISTS public.idx_market_ads_pickup_location_published;
ALTER TABLE IF EXISTS public.market_ads
    DROP CONSTRAINT IF EXISTS market_ads_pickup_confirmed_by_fkey,
    DROP CONSTRAINT IF EXISTS chk_market_ads_pickup_location_source,
    DROP CONSTRAINT IF EXISTS chk_market_ads_pickup_longitude,
    DROP CONSTRAINT IF EXISTS chk_market_ads_pickup_latitude,
    DROP COLUMN IF EXISTS pickup_confirmed_at,
    DROP COLUMN IF EXISTS pickup_confirmed_by,
    DROP COLUMN IF EXISTS pickup_location_source,
    DROP COLUMN IF EXISTS pickup_gps_accuracy_meters,
    DROP COLUMN IF EXISTS pickup_location,
    DROP COLUMN IF EXISTS pickup_longitude,
    DROP COLUMN IF EXISTS pickup_latitude,
    DROP COLUMN IF EXISTS pickup_landmark,
    DROP COLUMN IF EXISTS pickup_address;

DROP INDEX IF EXISTS public.idx_nursery_addresses_location;
ALTER TABLE public.nursery_addresses
    DROP CONSTRAINT IF EXISTS nursery_addresses_location_confirmed_by_fkey,
    DROP CONSTRAINT IF EXISTS chk_nursery_addresses_location_source,
    DROP CONSTRAINT IF EXISTS chk_nursery_addresses_longitude,
    DROP CONSTRAINT IF EXISTS chk_nursery_addresses_latitude,
    DROP COLUMN IF EXISTS location_confirmed_at,
    DROP COLUMN IF EXISTS location_confirmed_by,
    DROP COLUMN IF EXISTS location_source,
    DROP COLUMN IF EXISTS landmark,
    DROP COLUMN IF EXISTS gps_accuracy_meters,
    DROP COLUMN IF EXISTS location;

DROP INDEX IF EXISTS public.idx_user_addresses_location;
ALTER TABLE public.user_addresses
    DROP CONSTRAINT IF EXISTS user_addresses_location_confirmed_by_fkey,
    DROP CONSTRAINT IF EXISTS chk_user_addresses_location_source,
    DROP CONSTRAINT IF EXISTS chk_user_addresses_longitude,
    DROP CONSTRAINT IF EXISTS chk_user_addresses_latitude,
    DROP COLUMN IF EXISTS location_confirmed_at,
    DROP COLUMN IF EXISTS location_confirmed_by,
    DROP COLUMN IF EXISTS location_source,
    DROP COLUMN IF EXISTS landmark,
    DROP COLUMN IF EXISTS gps_accuracy_meters,
    DROP COLUMN IF EXISTS location;
