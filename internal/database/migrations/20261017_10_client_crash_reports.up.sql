-- =============================================================================
-- Migration 2026-10-17 010: crash reports from the mobile and web apps
-- =============================================================================
-- Uncaught app errors are sent to POST /api/v1/app/crash-reports and kept in
-- our own database (no third-party crash service: no account needed, and
-- device data stays under our DPDP controls). The API scrubs tokens, mobile
-- numbers and email addresses and caps every field before storing.
-- No IP address is stored. Append-only; retention per
-- greenroot-infra/docs/operations/database-operations.md §5.
-- =============================================================================

CREATE TABLE IF NOT EXISTS public.client_crash_reports (
    client_crash_report_id bigserial PRIMARY KEY,
    user_id                bigint REFERENCES public.users(user_id) ON DELETE SET NULL,
    platform               text NOT NULL,
    app_version            text NOT NULL,
    build_number           text,
    os_version             text,
    device_model           text,
    error_type             text NOT NULL,
    message                text NOT NULL,
    stack_trace            text,
    context                jsonb NOT NULL DEFAULT '{}'::jsonb,
    is_fatal               boolean NOT NULL DEFAULT false,
    occurred_at            timestamp with time zone NOT NULL,
    received_at            timestamp with time zone NOT NULL DEFAULT now(),
    request_id             text,
    CONSTRAINT ck_client_crash_reports_platform CHECK (platform IN ('android', 'ios', 'web')),
    CONSTRAINT ck_client_crash_reports_sizes CHECK (
            length(app_version) <= 32
        AND length(error_type) <= 200
        AND length(message) <= 2000
        AND (stack_trace IS NULL OR length(stack_trace) <= 16000)
        AND pg_column_size(context) <= 8192)
);

CREATE INDEX IF NOT EXISTS idx_client_crash_reports_received_at ON public.client_crash_reports (received_at DESC);
CREATE INDEX IF NOT EXISTS idx_client_crash_reports_app_version ON public.client_crash_reports (app_version, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_client_crash_reports_user_id ON public.client_crash_reports (user_id) WHERE user_id IS NOT NULL;
