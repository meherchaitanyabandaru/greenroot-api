-- The trip QR is the driver's invitation (2026-10-28): any approved driver
-- may take a trip; nurseries no longer keep their own drivers.
-- required_vehicle_type: the lorry the trip needs (NULL = any).
-- invite_expires_at: the QR stops working 72 hours after it was issued
-- (NULL for trips made before this, which never expire).
ALTER TABLE public.dispatches ADD COLUMN IF NOT EXISTS required_vehicle_type varchar(20);
ALTER TABLE public.dispatches ADD COLUMN IF NOT EXISTS invite_expires_at timestamptz;
ALTER TABLE public.dispatches DROP CONSTRAINT IF EXISTS ck_dispatches_required_vehicle_type;
ALTER TABLE public.dispatches ADD CONSTRAINT ck_dispatches_required_vehicle_type
  CHECK (required_vehicle_type IS NULL OR required_vehicle_type IN ('TRUCK', 'VAN', 'PICKUP', 'AUTO'));
COMMENT ON COLUMN public.dispatches.required_vehicle_type IS 'Lorry the trip needs (TRUCK, VAN, PICKUP, AUTO); NULL = any';
COMMENT ON COLUMN public.dispatches.invite_expires_at IS 'Trip QR (driver invitation) stops working after this; NULL = never';
