DROP INDEX IF EXISTS public.idx_notifications_push_attempted_at;
ALTER TABLE public.notifications DROP CONSTRAINT IF EXISTS ck_notifications_push_status;
ALTER TABLE public.notifications DROP COLUMN IF EXISTS push_attempted_at, DROP COLUMN IF EXISTS push_status;
