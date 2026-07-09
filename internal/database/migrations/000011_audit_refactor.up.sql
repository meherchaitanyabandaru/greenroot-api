-- =============================================================================
-- Migration 000011: Enterprise Audit Logging Refactor
-- =============================================================================
-- Extends audit_logs with request correlation, nursery scoping, typed module/
-- entity fields, and a human-readable description.
-- Adds security_audit_logs for auth/security events.
-- All changes are additive — no existing data is lost.
-- =============================================================================

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. Relax legacy NOT NULL constraints so old columns stay optional going
--    forward (new writes use module/entity_type instead of table_name/record_id)
-- ---------------------------------------------------------------------------
ALTER TABLE public.audit_logs
    ALTER COLUMN table_name  DROP NOT NULL,
    ALTER COLUMN record_id   DROP NOT NULL,
    ALTER COLUMN action_type DROP NOT NULL;

-- Widen action_type — new typed constants (e.g. "SUBSCRIPTION_BLOCKED") can
-- be up to 50 chars; the old VARCHAR(20) was too narrow.
ALTER TABLE public.audit_logs
    ALTER COLUMN action_type TYPE VARCHAR(50);

-- ---------------------------------------------------------------------------
-- 2. Add new structured columns
-- ---------------------------------------------------------------------------
ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_id  VARCHAR(100),  -- X-Request-Id for cross-log correlation
    ADD COLUMN IF NOT EXISTS user_id     BIGINT,        -- actor (mirrors changed_by; backfilled below)
    ADD COLUMN IF NOT EXISTS nursery_id  BIGINT,        -- tenant scope
    ADD COLUMN IF NOT EXISTS module      VARCHAR(50),   -- typed: ORDERS, AUTH, PLANTS …
    ADD COLUMN IF NOT EXISTS entity_type VARCHAR(100),  -- typed: order, order_item, plant …
    ADD COLUMN IF NOT EXISTS description TEXT,          -- human-readable summary
    ADD COLUMN IF NOT EXISTS device_info JSONB,         -- structured device / OS / app-version
    ADD COLUMN IF NOT EXISTS metadata    JSONB;         -- catch-all extra context

-- ---------------------------------------------------------------------------
-- 3. Backfill: user_id = changed_by for all existing rows
-- ---------------------------------------------------------------------------
UPDATE public.audit_logs
SET    user_id = changed_by
WHERE  changed_by IS NOT NULL
  AND  user_id   IS NULL;

-- ---------------------------------------------------------------------------
-- 4. Indexes — covering the most common admin query patterns
--    Partial WHERE clauses keep index size small by excluding NULL rows
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_audit_logs_changed_at  ON public.audit_logs (changed_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_request_id  ON public.audit_logs (request_id)  WHERE request_id  IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id     ON public.audit_logs (user_id)     WHERE user_id     IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_nursery_id  ON public.audit_logs (nursery_id)  WHERE nursery_id  IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_module      ON public.audit_logs (module)      WHERE module      IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_entity      ON public.audit_logs (entity_type, record_id) WHERE entity_type IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 5. security_audit_logs — dedicated table for auth / security events
--    Kept separate so admin queries on business events stay fast
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.security_audit_logs (
    id          BIGSERIAL    PRIMARY KEY,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    request_id  VARCHAR(100),
    user_id     BIGINT,
    nursery_id  BIGINT,
    event_type  VARCHAR(100) NOT NULL,
    description TEXT,
    metadata    JSONB,
    ip_address  VARCHAR(100),
    device_info JSONB
);

CREATE INDEX IF NOT EXISTS idx_sec_audit_created_at  ON public.security_audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sec_audit_user_id     ON public.security_audit_logs (user_id)    WHERE user_id    IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sec_audit_nursery_id  ON public.security_audit_logs (nursery_id) WHERE nursery_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sec_audit_event_type  ON public.security_audit_logs (event_type);
CREATE INDEX IF NOT EXISTS idx_sec_audit_request_id  ON public.security_audit_logs (request_id) WHERE request_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Retention note (enforce externally — DO NOT add automatic deletes here):
--   • Keep last 12 months in primary DB.
--   • Archive older rows to cold storage (S3 / GCS Parquet) monthly.
--   • Hard-delete from primary DB only after business/legal retention period.
--   • Partition by changed_at / created_at when table exceeds ~10M rows:
--       PARTITION BY RANGE (changed_at)  — schema is ready; add PARTITION OF later.
-- ---------------------------------------------------------------------------

COMMIT;
