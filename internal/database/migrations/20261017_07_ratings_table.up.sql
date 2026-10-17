-- =============================================================================
-- Migration 2026-10-17 007: restore the ratings table
-- =============================================================================
-- The ratings module (app, trip and order ratings) is mounted at
-- /api/v1/ratings, but its table was lost when the old infra migrations were
-- consolidated (greenroot-infra b120d07 dropped migration 012), so every
-- ratings request failed on a fresh install. Found by cmd/sqlcheck.
-- Same definition as infra bcd1344, with named constraints.
-- =============================================================================

CREATE TABLE IF NOT EXISTS public.ratings (
    rating_id                 bigserial PRIMARY KEY,
    rating_type               text NOT NULL,
    rated_by_user_id          bigint NOT NULL REFERENCES public.users(user_id) ON DELETE CASCADE,
    order_id                  bigint REFERENCES public.orders(order_id) ON DELETE SET NULL,
    dispatch_id               bigint REFERENCES public.dispatches(dispatch_id) ON DELETE SET NULL,
    overall_rating            smallint,
    would_recommend           boolean,
    driver_behaviour_rating   smallint,
    on_time_delivery_rating   smallint,
    plant_condition_rating    smallint,
    plant_quality_rating      smallint,
    communication_rating      smallint,
    overall_experience_rating smallint,
    would_buy_again           boolean,
    comment                   text,
    created_at                timestamp with time zone NOT NULL DEFAULT now(),
    updated_at                timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT ck_ratings_type CHECK (rating_type IN ('APP', 'TRIP', 'ORDER')),
    CONSTRAINT ck_ratings_scores CHECK (
            (overall_rating            IS NULL OR overall_rating            BETWEEN 1 AND 5)
        AND (driver_behaviour_rating   IS NULL OR driver_behaviour_rating   BETWEEN 1 AND 5)
        AND (on_time_delivery_rating   IS NULL OR on_time_delivery_rating   BETWEEN 1 AND 5)
        AND (plant_condition_rating    IS NULL OR plant_condition_rating    BETWEEN 1 AND 5)
        AND (plant_quality_rating      IS NULL OR plant_quality_rating      BETWEEN 1 AND 5)
        AND (communication_rating      IS NULL OR communication_rating      BETWEEN 1 AND 5)
        AND (overall_experience_rating IS NULL OR overall_experience_rating BETWEEN 1 AND 5))
);

-- One rating per user per subject; the repository upserts on these.
CREATE UNIQUE INDEX IF NOT EXISTS uq_ratings_app   ON public.ratings (rated_by_user_id) WHERE rating_type = 'APP';
CREATE UNIQUE INDEX IF NOT EXISTS uq_ratings_order ON public.ratings (rated_by_user_id, order_id) WHERE rating_type = 'ORDER' AND order_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_ratings_trip  ON public.ratings (rated_by_user_id, dispatch_id) WHERE rating_type = 'TRIP' AND dispatch_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ratings_order    ON public.ratings (order_id)    WHERE order_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ratings_dispatch ON public.ratings (dispatch_id) WHERE dispatch_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ratings_user     ON public.ratings (rated_by_user_id);
