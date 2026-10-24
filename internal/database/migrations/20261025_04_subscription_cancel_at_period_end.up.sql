-- Cancelling keeps what was paid for: the subscription stays usable until
-- end_date, then becomes CANCELLED (not EXPIRED). Set when the owner asks to
-- cancel; cleared by a renewal.
ALTER TABLE public.user_subscriptions ADD COLUMN IF NOT EXISTS cancel_requested_at timestamp with time zone;
COMMENT ON COLUMN public.user_subscriptions.cancel_requested_at IS 'Owner asked to cancel at the end of the paid period; NULL after a renewal';
