DROP INDEX IF EXISTS public.idx_user_subscriptions_subscription_code_unique;
DROP INDEX IF EXISTS public.idx_notifications_notification_code_unique;
DROP INDEX IF EXISTS public.idx_attachments_attachment_code_unique;
DROP INDEX IF EXISTS public.idx_vehicles_vehicle_code_unique;
DROP INDEX IF EXISTS public.idx_drivers_driver_code_unique;
DROP INDEX IF EXISTS public.idx_payments_payment_code_unique;
DROP INDEX IF EXISTS public.idx_dispatches_dispatch_code_unique;
DROP INDEX IF EXISTS public.idx_orders_order_code_unique;
DROP INDEX IF EXISTS public.idx_plant_requests_request_code_unique;
DROP INDEX IF EXISTS public.idx_nursery_inventory_inventory_code_unique;
DROP INDEX IF EXISTS public.idx_nurseries_nursery_code_unique;
DROP INDEX IF EXISTS public.idx_plants_plant_code_unique;
DROP INDEX IF EXISTS public.idx_users_user_code_unique;

ALTER TABLE IF EXISTS public.user_subscriptions ALTER COLUMN subscription_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.notifications ALTER COLUMN notification_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.attachments ALTER COLUMN attachment_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.vehicles ALTER COLUMN vehicle_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.drivers ALTER COLUMN driver_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.payments ALTER COLUMN payment_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.dispatches ALTER COLUMN dispatch_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.orders ALTER COLUMN order_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.plant_requests ALTER COLUMN request_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.nursery_inventory ALTER COLUMN inventory_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.nurseries ALTER COLUMN nursery_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.plants ALTER COLUMN plant_code DROP DEFAULT;
ALTER TABLE IF EXISTS public.users ALTER COLUMN user_code DROP DEFAULT;

ALTER TABLE IF EXISTS public.user_subscriptions DROP COLUMN IF EXISTS subscription_code;
ALTER TABLE IF EXISTS public.notifications DROP COLUMN IF EXISTS notification_code;
ALTER TABLE IF EXISTS public.attachments DROP COLUMN IF EXISTS attachment_code;
ALTER TABLE IF EXISTS public.vehicles DROP COLUMN IF EXISTS vehicle_code;
ALTER TABLE IF EXISTS public.drivers DROP COLUMN IF EXISTS driver_code;
ALTER TABLE IF EXISTS public.payments DROP COLUMN IF EXISTS payment_code;
ALTER TABLE IF EXISTS public.dispatches DROP COLUMN IF EXISTS dispatch_code;
ALTER TABLE IF EXISTS public.orders DROP COLUMN IF EXISTS order_code;
ALTER TABLE IF EXISTS public.plant_requests DROP COLUMN IF EXISTS request_code;
ALTER TABLE IF EXISTS public.nursery_inventory DROP COLUMN IF EXISTS inventory_code;
ALTER TABLE IF EXISTS public.plants DROP COLUMN IF EXISTS plant_code;
ALTER TABLE IF EXISTS public.users DROP COLUMN IF EXISTS user_code;

ALTER TABLE IF EXISTS public.nurseries ALTER COLUMN nursery_code DROP NOT NULL;

DROP FUNCTION IF EXISTS public.next_public_code(text, text, integer, boolean, timestamp without time zone);
DROP TABLE IF EXISTS public.public_code_sequences;
