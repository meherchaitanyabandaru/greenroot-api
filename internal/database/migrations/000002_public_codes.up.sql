-- Add stable, human-readable public codes for admin, business, and support workflows.
-- Internal BIGINT primary keys remain the source of truth for joins and foreign keys.

CREATE TABLE IF NOT EXISTS public.public_code_sequences (
    code_key character varying(40) NOT NULL,
    date_key character varying(8) NOT NULL DEFAULT '',
    last_value bigint NOT NULL DEFAULT 0,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT public_code_sequences_pkey PRIMARY KEY (code_key, date_key)
);

CREATE OR REPLACE FUNCTION public.next_public_code(
    p_code_key text,
    p_prefix text,
    p_width integer,
    p_date_based boolean DEFAULT false,
    p_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
) RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    v_date_key text := '';
    v_last_value bigint;
BEGIN
    IF p_date_based THEN
        v_date_key := to_char(p_at::date, 'YYYYMMDD');
    END IF;

    INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
    VALUES (p_code_key, v_date_key, 1)
    ON CONFLICT (code_key, date_key)
    DO UPDATE SET last_value = public.public_code_sequences.last_value + 1,
                  updated_at = CURRENT_TIMESTAMP
    RETURNING last_value INTO v_last_value;

    IF p_date_based THEN
        RETURN p_prefix || '-' || v_date_key || '-' || lpad(v_last_value::text, p_width, '0');
    END IF;

    RETURN p_prefix || '-' || lpad(v_last_value::text, p_width, '0');
END;
$$;

ALTER TABLE public.users ADD COLUMN IF NOT EXISTS user_code character varying(20);
ALTER TABLE public.plants ADD COLUMN IF NOT EXISTS plant_code character varying(20);
ALTER TABLE public.nurseries ADD COLUMN IF NOT EXISTS nursery_code character varying(50);
ALTER TABLE public.nursery_inventory ADD COLUMN IF NOT EXISTS inventory_code character varying(20);
ALTER TABLE public.plant_requests ADD COLUMN IF NOT EXISTS request_code character varying(30);
ALTER TABLE public.orders ADD COLUMN IF NOT EXISTS order_code character varying(30);
ALTER TABLE public.dispatches ADD COLUMN IF NOT EXISTS dispatch_code character varying(30);
ALTER TABLE public.payments ADD COLUMN IF NOT EXISTS payment_code character varying(30);
ALTER TABLE public.drivers ADD COLUMN IF NOT EXISTS driver_code character varying(20);
ALTER TABLE public.vehicles ADD COLUMN IF NOT EXISTS vehicle_code character varying(20);
ALTER TABLE public.attachments ADD COLUMN IF NOT EXISTS attachment_code character varying(20);
ALTER TABLE public.notifications ADD COLUMN IF NOT EXISTS notification_code character varying(20);
ALTER TABLE public.user_subscriptions ADD COLUMN IF NOT EXISTS subscription_code character varying(20);

ALTER TABLE public.users ALTER COLUMN user_code SET DEFAULT public.next_public_code('users', 'USR', 6, false);
ALTER TABLE public.plants ALTER COLUMN plant_code SET DEFAULT public.next_public_code('plants', 'PLT', 6, false);
ALTER TABLE public.nurseries ALTER COLUMN nursery_code SET DEFAULT public.next_public_code('nurseries', 'NUR', 6, false);
ALTER TABLE public.nursery_inventory ALTER COLUMN inventory_code SET DEFAULT public.next_public_code('nursery_inventory', 'INV', 6, false);
ALTER TABLE public.plant_requests ALTER COLUMN request_code SET DEFAULT public.next_public_code('plant_requests', 'REQ', 4, true);
ALTER TABLE public.orders ALTER COLUMN order_code SET DEFAULT public.next_public_code('orders', 'ORD', 4, true);
ALTER TABLE public.dispatches ALTER COLUMN dispatch_code SET DEFAULT public.next_public_code('dispatches', 'DSP', 4, true);
ALTER TABLE public.payments ALTER COLUMN payment_code SET DEFAULT public.next_public_code('payments', 'PAY', 4, true);
ALTER TABLE public.drivers ALTER COLUMN driver_code SET DEFAULT public.next_public_code('drivers', 'DRV', 6, false);
ALTER TABLE public.vehicles ALTER COLUMN vehicle_code SET DEFAULT public.next_public_code('vehicles', 'VEH', 6, false);
ALTER TABLE public.attachments ALTER COLUMN attachment_code SET DEFAULT public.next_public_code('attachments', 'ATT', 6, false);
ALTER TABLE public.notifications ALTER COLUMN notification_code SET DEFAULT public.next_public_code('notifications', 'NTF', 6, false);
ALTER TABLE public.user_subscriptions ALTER COLUMN subscription_code SET DEFAULT public.next_public_code('user_subscriptions', 'SUB', 6, false);

WITH numbered AS (
    SELECT user_id, row_number() OVER (ORDER BY user_id) AS seq
    FROM public.users
)
UPDATE public.users u
SET user_code = 'USR-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE u.user_id = numbered.user_id;

WITH numbered AS (
    SELECT plant_id, row_number() OVER (ORDER BY plant_id) AS seq
    FROM public.plants
)
UPDATE public.plants p
SET plant_code = 'PLT-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE p.plant_id = numbered.plant_id;

WITH numbered AS (
    SELECT nursery_id, row_number() OVER (ORDER BY nursery_id) AS seq
    FROM public.nurseries
)
UPDATE public.nurseries n
SET nursery_code = 'NUR-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE n.nursery_id = numbered.nursery_id;

WITH numbered AS (
    SELECT inventory_id, row_number() OVER (ORDER BY inventory_id) AS seq
    FROM public.nursery_inventory
)
UPDATE public.nursery_inventory ni
SET inventory_code = 'INV-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE ni.inventory_id = numbered.inventory_id;

WITH numbered AS (
    SELECT request_id,
           to_char(created_at::date, 'YYYYMMDD') AS date_key,
           row_number() OVER (PARTITION BY created_at::date ORDER BY request_id) AS seq
    FROM public.plant_requests
)
UPDATE public.plant_requests pr
SET request_code = 'REQ-' || numbered.date_key || '-' || lpad(numbered.seq::text, 4, '0')
FROM numbered
WHERE pr.request_id = numbered.request_id;

WITH numbered AS (
    SELECT order_id,
           to_char(order_date::date, 'YYYYMMDD') AS date_key,
           row_number() OVER (PARTITION BY order_date::date ORDER BY order_id) AS seq
    FROM public.orders
)
UPDATE public.orders o
SET order_code = 'ORD-' || numbered.date_key || '-' || lpad(numbered.seq::text, 4, '0')
FROM numbered
WHERE o.order_id = numbered.order_id;

WITH numbered AS (
    SELECT dispatch_id,
           to_char(created_at::date, 'YYYYMMDD') AS date_key,
           row_number() OVER (PARTITION BY created_at::date ORDER BY dispatch_id) AS seq
    FROM public.dispatches
)
UPDATE public.dispatches d
SET dispatch_code = 'DSP-' || numbered.date_key || '-' || lpad(numbered.seq::text, 4, '0')
FROM numbered
WHERE d.dispatch_id = numbered.dispatch_id;

WITH numbered AS (
    SELECT payment_id,
           to_char(created_at::date, 'YYYYMMDD') AS date_key,
           row_number() OVER (PARTITION BY created_at::date ORDER BY payment_id) AS seq
    FROM public.payments
)
UPDATE public.payments p
SET payment_code = 'PAY-' || numbered.date_key || '-' || lpad(numbered.seq::text, 4, '0')
FROM numbered
WHERE p.payment_id = numbered.payment_id;

WITH numbered AS (
    SELECT driver_id, row_number() OVER (ORDER BY driver_id) AS seq
    FROM public.drivers
)
UPDATE public.drivers d
SET driver_code = 'DRV-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE d.driver_id = numbered.driver_id;

WITH numbered AS (
    SELECT vehicle_id, row_number() OVER (ORDER BY vehicle_id) AS seq
    FROM public.vehicles
)
UPDATE public.vehicles v
SET vehicle_code = 'VEH-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE v.vehicle_id = numbered.vehicle_id;

WITH numbered AS (
    SELECT attachment_id, row_number() OVER (ORDER BY attachment_id) AS seq
    FROM public.attachments
)
UPDATE public.attachments a
SET attachment_code = 'ATT-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE a.attachment_id = numbered.attachment_id;

WITH numbered AS (
    SELECT notification_id, row_number() OVER (ORDER BY notification_id) AS seq
    FROM public.notifications
)
UPDATE public.notifications n
SET notification_code = 'NTF-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE n.notification_id = numbered.notification_id;

WITH numbered AS (
    SELECT user_subscription_id, row_number() OVER (ORDER BY user_subscription_id) AS seq
    FROM public.user_subscriptions
)
UPDATE public.user_subscriptions us
SET subscription_code = 'SUB-' || lpad(numbered.seq::text, 6, '0')
FROM numbered
WHERE us.user_subscription_id = numbered.user_subscription_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_user_code_unique ON public.users (user_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_plants_plant_code_unique ON public.plants (plant_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_nurseries_nursery_code_unique ON public.nurseries (nursery_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_nursery_inventory_inventory_code_unique ON public.nursery_inventory (inventory_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_plant_requests_request_code_unique ON public.plant_requests (request_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_order_code_unique ON public.orders (order_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dispatches_dispatch_code_unique ON public.dispatches (dispatch_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_payment_code_unique ON public.payments (payment_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_drivers_driver_code_unique ON public.drivers (driver_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vehicles_vehicle_code_unique ON public.vehicles (vehicle_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_attachments_attachment_code_unique ON public.attachments (attachment_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_notification_code_unique ON public.notifications (notification_code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_subscriptions_subscription_code_unique ON public.user_subscriptions (subscription_code);

ALTER TABLE public.users ALTER COLUMN user_code SET NOT NULL;
ALTER TABLE public.plants ALTER COLUMN plant_code SET NOT NULL;
ALTER TABLE public.nurseries ALTER COLUMN nursery_code SET NOT NULL;
ALTER TABLE public.nursery_inventory ALTER COLUMN inventory_code SET NOT NULL;
ALTER TABLE public.plant_requests ALTER COLUMN request_code SET NOT NULL;
ALTER TABLE public.orders ALTER COLUMN order_code SET NOT NULL;
ALTER TABLE public.dispatches ALTER COLUMN dispatch_code SET NOT NULL;
ALTER TABLE public.payments ALTER COLUMN payment_code SET NOT NULL;
ALTER TABLE public.drivers ALTER COLUMN driver_code SET NOT NULL;
ALTER TABLE public.vehicles ALTER COLUMN vehicle_code SET NOT NULL;
ALTER TABLE public.attachments ALTER COLUMN attachment_code SET NOT NULL;
ALTER TABLE public.notifications ALTER COLUMN notification_code SET NOT NULL;
ALTER TABLE public.user_subscriptions ALTER COLUMN subscription_code SET NOT NULL;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'users', '', count(*) FROM public.users
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'plants', '', count(*) FROM public.plants
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'nurseries', '', count(*) FROM public.nurseries
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'nursery_inventory', '', count(*) FROM public.nursery_inventory
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'drivers', '', count(*) FROM public.drivers
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'vehicles', '', count(*) FROM public.vehicles
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'attachments', '', count(*) FROM public.attachments
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'notifications', '', count(*) FROM public.notifications
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'user_subscriptions', '', count(*) FROM public.user_subscriptions
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'plant_requests', to_char(created_at::date, 'YYYYMMDD'), count(*)
FROM public.plant_requests
GROUP BY created_at::date
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'orders', to_char(order_date::date, 'YYYYMMDD'), count(*)
FROM public.orders
GROUP BY order_date::date
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'dispatches', to_char(created_at::date, 'YYYYMMDD'), count(*)
FROM public.dispatches
GROUP BY created_at::date
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'payments', to_char(created_at::date, 'YYYYMMDD'), count(*)
FROM public.payments
GROUP BY created_at::date
ON CONFLICT (code_key, date_key) DO UPDATE SET last_value = EXCLUDED.last_value, updated_at = CURRENT_TIMESTAMP;
