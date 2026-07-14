DROP INDEX IF EXISTS public.uq_driver_one_active_trip;

CREATE UNIQUE INDEX IF NOT EXISTS uq_driver_one_active_trip
	ON public.dispatches (driver_user_id)
	WHERE dispatch_status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT');
