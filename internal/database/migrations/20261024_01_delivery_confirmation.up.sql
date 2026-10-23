-- Delivery confirmation: a trip is DELIVERED only with the customer's
-- confirmation (their delivery code, or "I received it" in their app), or as
-- an audited owner override that the customer can still dispute for 48 hours.
-- Trips can also go on an outside lorry whose driver has no app.
ALTER TABLE public.dispatches
    ADD COLUMN IF NOT EXISTS is_outside_transport boolean DEFAULT false NOT NULL,
    ADD COLUMN IF NOT EXISTS outside_vehicle_number character varying(20),
    ADD COLUMN IF NOT EXISTS outside_driver_name character varying(100),
    ADD COLUMN IF NOT EXISTS outside_driver_mobile character varying(15),
    ADD COLUMN IF NOT EXISTS delivery_confirmation character varying(20),
    ADD COLUMN IF NOT EXISTS delivery_confirmed_by_user_id bigint REFERENCES public.users(user_id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS delivery_confirmed_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS delivery_override_reason text,
    ADD COLUMN IF NOT EXISTS delivery_issue text,
    ADD COLUMN IF NOT EXISTS delivery_issue_reported_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS delivery_code_attempts smallint DEFAULT 0 NOT NULL;

-- Trips delivered before this existed had no confirmation.
UPDATE public.dispatches
SET delivery_confirmation = 'UNCONFIRMED',
    delivery_override_reason = 'Delivered before delivery confirmation existed'
WHERE status = 'DELIVERED' AND delivery_confirmation IS NULL;

-- The customer snapshot was never filled; it decides who sees the code.
UPDATE public.dispatches d
SET customer_user_id = o.buyer_user_id
FROM public.orders o
WHERE o.order_id = d.order_id AND d.customer_user_id IS NULL AND o.buyer_user_id IS NOT NULL;

ALTER TABLE public.dispatches
    ADD CONSTRAINT ck_dispatches_delivery_confirmation
        CHECK (delivery_confirmation IN ('CUSTOMER_CODE', 'CUSTOMER_APP', 'UNCONFIRMED')),
    ADD CONSTRAINT ck_dispatches_delivered_is_confirmed
        CHECK (status <> 'DELIVERED' OR delivery_confirmation IS NOT NULL),
    ADD CONSTRAINT ck_dispatches_unconfirmed_has_reason
        CHECK (delivery_confirmation <> 'UNCONFIRMED' OR delivery_override_reason IS NOT NULL),
    ADD CONSTRAINT ck_dispatches_outside_has_vehicle
        CHECK (NOT is_outside_transport OR outside_vehicle_number IS NOT NULL);

COMMENT ON COLUMN public.dispatches.is_outside_transport IS 'Outside lorry: driver has no app, no QR join or GPS; the nursery confirms delivery with the customer''s code';
COMMENT ON COLUMN public.dispatches.delivery_confirmation IS 'How delivery was confirmed: CUSTOMER_CODE (code entered by driver/nursery), CUSTOMER_APP (customer tapped received), UNCONFIRMED (nursery override with reason)';
COMMENT ON COLUMN public.dispatches.delivery_code_attempts IS 'Wrong delivery codes entered; at 5 the code stops working for this trip';
COMMENT ON COLUMN public.dispatches.delivery_issue IS 'Problem the customer reported within 48 hours of delivery';
