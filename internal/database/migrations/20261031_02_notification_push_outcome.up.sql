-- Push outcome on its own (2026-10-31). notifications.status/sent_at describe
-- the in-app inbox (created SENT, then READ), so they cannot tell whether a
-- push reached a phone. The push sender records its outcome here; reading the
-- notification leaves it alone. Feeds the admin push status page.
ALTER TABLE public.notifications
    ADD COLUMN IF NOT EXISTS push_status character varying(20),
    ADD COLUMN IF NOT EXISTS push_attempted_at timestamp with time zone;

ALTER TABLE public.notifications DROP CONSTRAINT IF EXISTS ck_notifications_push_status;
ALTER TABLE public.notifications ADD CONSTRAINT ck_notifications_push_status
    CHECK (push_status IS NULL OR push_status IN ('SENT', 'FAILED', 'NO_DEVICE'));

COMMENT ON COLUMN public.notifications.push_status IS 'Push delivery outcome (SENT, FAILED, NO_DEVICE); NULL = no push tried';
COMMENT ON COLUMN public.notifications.push_attempted_at IS 'When the push sender recorded push_status';

CREATE INDEX IF NOT EXISTS idx_notifications_push_attempted_at
    ON public.notifications (push_attempted_at) WHERE push_attempted_at IS NOT NULL;
