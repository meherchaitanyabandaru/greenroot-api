ALTER TABLE public.dispatches DROP CONSTRAINT IF EXISTS ck_dispatches_required_vehicle_type;
ALTER TABLE public.dispatches DROP COLUMN IF EXISTS invite_expires_at;
ALTER TABLE public.dispatches DROP COLUMN IF EXISTS required_vehicle_type;
