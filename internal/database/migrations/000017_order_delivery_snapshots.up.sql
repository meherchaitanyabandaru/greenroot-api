-- ============================================================
-- 000017 — Order Delivery Snapshots
-- Order-specific delivery destination. This deliberately does
-- not update the buyer/customer profile address.
-- ============================================================

CREATE TABLE IF NOT EXISTS public.order_delivery_snapshots (
    snapshot_id                 BIGSERIAL PRIMARY KEY,
    order_id                    BIGINT NOT NULL UNIQUE REFERENCES public.orders(order_id) ON DELETE CASCADE,
    contact_name                VARCHAR(100),
    contact_mobile              VARCHAR(20),
    alternate_mobile            VARCHAR(20),
    address_line1               VARCHAR(255),
    address_line2               VARCHAR(255),
    city                        VARCHAR(100),
    state                       VARCHAR(100),
    country                     VARCHAR(100),
    postal_code                 VARCHAR(20),
    landmark                    VARCHAR(255),
    delivery_instructions       TEXT,
    latitude                    NUMERIC(10,7),
    longitude                   NUMERIC(10,7),
    location                    GEOGRAPHY(POINT, 4326),
    gps_accuracy_meters         NUMERIC(8,2),
    location_source             VARCHAR(40),
    confirmed_by                BIGINT REFERENCES public.users(user_id),
    confirmed_at                TIMESTAMP,
    emergency_updated           BOOLEAN NOT NULL DEFAULT false,
    requires_driver_ack         BOOLEAN NOT NULL DEFAULT false,
    driver_acknowledged_by      BIGINT REFERENCES public.users(user_id),
    driver_acknowledged_at      TIMESTAMP,
    created_at                  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_order_delivery_snapshot_latitude
        CHECK (latitude IS NULL OR (latitude >= -90 AND latitude <= 90)),
    CONSTRAINT chk_order_delivery_snapshot_longitude
        CHECK (longitude IS NULL OR (longitude >= -180 AND longitude <= 180)),
    CONSTRAINT chk_order_delivery_snapshot_location_source
        CHECK (
            location_source IS NULL OR location_source IN (
                'gps_confirmed',
                'nursery_default',
                'map_selected',
                'address_search',
                'admin_updated'
            )
        )
);

CREATE INDEX IF NOT EXISTS idx_order_delivery_snapshots_location
    ON public.order_delivery_snapshots USING GIST (location);

CREATE INDEX IF NOT EXISTS idx_order_delivery_snapshots_driver_ack
    ON public.order_delivery_snapshots (requires_driver_ack)
    WHERE requires_driver_ack = true;
