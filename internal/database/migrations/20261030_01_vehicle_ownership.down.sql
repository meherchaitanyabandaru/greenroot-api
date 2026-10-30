-- Manual rollback. Owner data stays in vehicles.owner_name/mobile (never removed).
DROP VIEW IF EXISTS public.driver_vehicle_eligibility;
DROP INDEX IF EXISTS public.idx_dispatches_vehicle_active;
DROP TABLE IF EXISTS public.vehicle_drivers;
ALTER TABLE public.vehicles DROP CONSTRAINT IF EXISTS ck_vehicles_approval_status;
ALTER TABLE public.vehicles DROP COLUMN IF EXISTS approval_note;
ALTER TABLE public.vehicles DROP COLUMN IF EXISTS approved_at;
ALTER TABLE public.vehicles DROP COLUMN IF EXISTS approved_by_user_id;
ALTER TABLE public.vehicles DROP COLUMN IF EXISTS approval_status;
ALTER TABLE public.vehicles DROP COLUMN IF EXISTS owner_id;
DROP TABLE IF EXISTS public.vehicle_owners;
