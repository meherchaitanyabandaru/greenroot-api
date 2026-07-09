CREATE TABLE IF NOT EXISTS public.subscription_promos (
    promo_id        BIGSERIAL PRIMARY KEY,
    promo_code      TEXT        NOT NULL UNIQUE,
    name            TEXT        NOT NULL,
    description     TEXT,
    discount_type   TEXT        NOT NULL CHECK (discount_type IN ('PERCENTAGE', 'FIXED')),
    discount_value  NUMERIC(10, 2) NOT NULL,
    max_discount_cap NUMERIC(10, 2),
    applicable_plans  JSONB     NOT NULL DEFAULT '[]',
    applicable_cycles JSONB     NOT NULL DEFAULT '[]',
    valid_from      DATE        NOT NULL,
    valid_until     DATE        NOT NULL,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    max_uses        BIGINT,
    used_count      INTEGER     NOT NULL DEFAULT 0,
    created_by      BIGINT      REFERENCES public.users(user_id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_subscription_promos_code    ON public.subscription_promos (promo_code);
CREATE INDEX IF NOT EXISTS idx_subscription_promos_active  ON public.subscription_promos (is_active, valid_from, valid_until);
