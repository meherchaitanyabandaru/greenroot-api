-- Migration 000011 rollback
BEGIN;

DROP TABLE IF EXISTS public.security_audit_logs;

DROP INDEX IF EXISTS idx_audit_logs_changed_at;
DROP INDEX IF EXISTS idx_audit_logs_request_id;
DROP INDEX IF EXISTS idx_audit_logs_user_id;
DROP INDEX IF EXISTS idx_audit_logs_nursery_id;
DROP INDEX IF EXISTS idx_audit_logs_module;
DROP INDEX IF EXISTS idx_audit_logs_entity;

ALTER TABLE public.audit_logs
    DROP COLUMN IF EXISTS request_id,
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS nursery_id,
    DROP COLUMN IF EXISTS module,
    DROP COLUMN IF EXISTS entity_type,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS device_info,
    DROP COLUMN IF EXISTS metadata;

ALTER TABLE public.audit_logs
    ALTER COLUMN action_type TYPE VARCHAR(20),
    ALTER COLUMN table_name  SET NOT NULL,
    ALTER COLUMN record_id   SET NOT NULL,
    ALTER COLUMN action_type SET NOT NULL;

COMMIT;
