-- =============================================================================
-- Migration 2026-10-17 001: the database enforces the domain rules
-- =============================================================================
-- Until now only Go code kept statuses valid and money/stock non-negative; a
-- bug or a manual UPDATE could store 'COMPLETE' or -5 units. These CHECKs make
-- such rows impossible. Each value list is the full set the code writes,
-- taken from the module's validator / status machine (named per constraint);
-- adding a status now needs a migration, which is intended.
--
-- Every existing row was checked against these rules before writing them.
-- Constraints are added NOT VALID (brief lock, no scan) and then validated
-- (scan under a lock that still allows reads and writes).
-- =============================================================================

-- ── Statuses (state machines) ───────────────────────────────────────────────

-- orders/policy.go + start-loading / complete-loading / cancel endpoints.
ALTER TABLE public.orders ADD CONSTRAINT ck_orders_status CHECK (order_status IN
    ('PENDING','CONFIRMED','LOADING','LOADED','PARTIALLY_FULFILLED','COMPLETED','CANCELLED')) NOT VALID;

-- payments/service.go isAllowedStatus / isAllowedMethod.
ALTER TABLE public.payments ADD CONSTRAINT ck_payments_status CHECK (payment_status IN
    ('PENDING','SUCCESS','FAILED','REFUNDED','CANCELLED')) NOT VALID;
ALTER TABLE public.payments ADD CONSTRAINT ck_payments_method CHECK (payment_method IS NULL OR payment_method IN
    ('UPI','CARD','CASH','BANK_TRANSFER','NET_BANKING','WALLET','COD','CHEQUE','OTHER')) NOT VALID;

-- dispatches/policy.go AllowedStatus.
ALTER TABLE public.dispatches ADD CONSTRAINT ck_dispatches_status CHECK (dispatch_status IN
    ('PENDING','ACCEPTED','DISPATCHED','IN_TRANSIT','DELIVERED','CANCELLED')) NOT VALID;

-- inventory/service.go isAllowedStatus.
ALTER TABLE public.nursery_inventory ADD CONSTRAINT ck_nursery_inventory_status CHECK (inventory_status IN
    ('AVAILABLE','LOW_STOCK','OUT_OF_STOCK','RESERVED','DISCONTINUED')) NOT VALID;

-- subscriptions/service.go isAllowedStatus + TRIAL (CreateTrialForOwner).
ALTER TABLE public.user_subscriptions ADD CONSTRAINT ck_user_subscriptions_status CHECK (subscription_status IN
    ('TRIAL','ACTIVE','PAUSED','CANCELLED','EXPIRED')) NOT VALID;

-- nurseries/service.go validateNursery / UpdateStatus.
ALTER TABLE public.nurseries ADD CONSTRAINT ck_nurseries_status CHECK (status IN
    ('ACTIVE','INACTIVE','SUSPENDED','DELETED','PENDING','APPROVED','REJECTED')) NOT VALID;

-- admin/service.go user status update.
ALTER TABLE public.users ADD CONSTRAINT ck_users_status CHECK (status IN
    ('ACTIVE','SUSPENDED','DELETED')) NOT VALID;

-- auth / users / nurseries session handling.
ALTER TABLE public.user_sessions ADD CONSTRAINT ck_user_sessions_status CHECK (session_status IN
    ('ACTIVE','LOGGED_OUT')) NOT VALID;

-- drivers/service.go and vehicles/service.go status validators.
ALTER TABLE public.drivers ADD CONSTRAINT ck_drivers_status CHECK (status IN
    ('ACTIVE','INACTIVE','SUSPENDED','DELETED')) NOT VALID;
ALTER TABLE public.vehicles ADD CONSTRAINT ck_vehicles_status CHECK (status IN
    ('ACTIVE','INACTIVE','MAINTENANCE','RETIRED')) NOT VALID;

-- invites/service.go allowedInviteTypes; status written by invites/repository.go.
ALTER TABLE public.invites ADD CONSTRAINT ck_invites_type CHECK (invite_type IN
    ('MANAGER_INVITE','DRIVER_INVITE','CUSTOMER_INVITE','NURSERY_ONBOARDING_INVITE','TRIP_SHARE_INVITE')) NOT VALID;
ALTER TABLE public.invites ADD CONSTRAINT ck_invites_status CHECK (status IN
    ('PENDING','ACCEPTED','CANCELLED')) NOT VALID;

-- plantrequests/service.go request and response validators.
ALTER TABLE public.plant_requests ADD CONSTRAINT ck_plant_requests_status CHECK (status IN
    ('DRAFT','OPEN','PARTIALLY_ACCEPTED','ACCEPTED','REJECTED','CLOSED')) NOT VALID;
ALTER TABLE public.plant_request_responses ADD CONSTRAINT ck_plant_request_responses_status CHECK (status IN
    ('AVAILABLE','PARTIAL','NOT_AVAILABLE','ACCEPTED','REJECTED')) NOT VALID;

-- notifications/service.go channel validator.
ALTER TABLE public.notifications ADD CONSTRAINT ck_notifications_channel CHECK (channel IN
    ('PUSH','SMS','EMAIL','IN_APP')) NOT VALID;

-- ── Money and quantities ────────────────────────────────────────────────────

ALTER TABLE public.payments ADD CONSTRAINT ck_payments_amount_positive CHECK (amount > 0) NOT VALID;
-- A payment belongs to exactly one thing, and payment_for says which.
ALTER TABLE public.payments ADD CONSTRAINT ck_payments_target CHECK (
       (payment_for = 'ORDER'        AND order_id IS NOT NULL AND user_subscription_id IS NULL)
    OR (payment_for = 'SUBSCRIPTION' AND user_subscription_id IS NOT NULL AND order_id IS NULL)) NOT VALID;

ALTER TABLE public.orders ADD CONSTRAINT ck_orders_total_non_negative CHECK (total_amount >= 0) NOT VALID;
ALTER TABLE public.order_items ADD CONSTRAINT ck_order_items_amounts CHECK (
        quantity > 0
    AND (unit_price IS NULL OR unit_price >= 0)
    AND (total_price IS NULL OR total_price >= 0)
    AND (loaded_quantity IS NULL OR (loaded_quantity >= 0 AND loaded_quantity <= quantity))) NOT VALID;

ALTER TABLE public.quotations ADD CONSTRAINT ck_quotations_total_non_negative CHECK (total_amount IS NULL OR total_amount >= 0) NOT VALID;
ALTER TABLE public.quotation_items ADD CONSTRAINT ck_quotation_items_amounts CHECK (
        quantity > 0
    AND (unit_price IS NULL OR unit_price >= 0)
    AND (total_price IS NULL OR total_price >= 0)) NOT VALID;

-- Stock can never go negative, whatever the code path.
ALTER TABLE public.nursery_inventory ADD CONSTRAINT ck_nursery_inventory_quantity CHECK (available_quantity >= 0) NOT VALID;
ALTER TABLE public.dispatch_items ADD CONSTRAINT ck_dispatch_items_quantity CHECK (quantity > 0) NOT VALID;

ALTER TABLE public.subscription_plans ADD CONSTRAINT ck_subscription_plans_prices CHECK (
    (monthly_price IS NULL OR monthly_price >= 0) AND (yearly_price IS NULL OR yearly_price >= 0)) NOT VALID;
ALTER TABLE public.subscription_promos ADD CONSTRAINT ck_subscription_promos_discount CHECK (
        discount_value > 0
    AND (discount_type <> 'PERCENTAGE' OR discount_value <= 100)
    AND (max_discount_cap IS NULL OR max_discount_cap >= 0)) NOT VALID;
ALTER TABLE public.market_ads ADD CONSTRAINT ck_market_ads_price CHECK (price_per_unit IS NULL OR price_per_unit >= 0) NOT VALID;

-- ── Validate (existing rows) ────────────────────────────────────────────────

ALTER TABLE public.orders                  VALIDATE CONSTRAINT ck_orders_status;
ALTER TABLE public.orders                  VALIDATE CONSTRAINT ck_orders_total_non_negative;
ALTER TABLE public.payments                VALIDATE CONSTRAINT ck_payments_status;
ALTER TABLE public.payments                VALIDATE CONSTRAINT ck_payments_method;
ALTER TABLE public.payments                VALIDATE CONSTRAINT ck_payments_amount_positive;
ALTER TABLE public.payments                VALIDATE CONSTRAINT ck_payments_target;
ALTER TABLE public.dispatches              VALIDATE CONSTRAINT ck_dispatches_status;
ALTER TABLE public.nursery_inventory       VALIDATE CONSTRAINT ck_nursery_inventory_status;
ALTER TABLE public.nursery_inventory       VALIDATE CONSTRAINT ck_nursery_inventory_quantity;
ALTER TABLE public.user_subscriptions      VALIDATE CONSTRAINT ck_user_subscriptions_status;
ALTER TABLE public.nurseries               VALIDATE CONSTRAINT ck_nurseries_status;
ALTER TABLE public.users                   VALIDATE CONSTRAINT ck_users_status;
ALTER TABLE public.user_sessions           VALIDATE CONSTRAINT ck_user_sessions_status;
ALTER TABLE public.drivers                 VALIDATE CONSTRAINT ck_drivers_status;
ALTER TABLE public.vehicles                VALIDATE CONSTRAINT ck_vehicles_status;
ALTER TABLE public.invites                 VALIDATE CONSTRAINT ck_invites_type;
ALTER TABLE public.invites                 VALIDATE CONSTRAINT ck_invites_status;
ALTER TABLE public.plant_requests          VALIDATE CONSTRAINT ck_plant_requests_status;
ALTER TABLE public.plant_request_responses VALIDATE CONSTRAINT ck_plant_request_responses_status;
ALTER TABLE public.notifications           VALIDATE CONSTRAINT ck_notifications_channel;
ALTER TABLE public.order_items             VALIDATE CONSTRAINT ck_order_items_amounts;
ALTER TABLE public.quotations              VALIDATE CONSTRAINT ck_quotations_total_non_negative;
ALTER TABLE public.quotation_items         VALIDATE CONSTRAINT ck_quotation_items_amounts;
ALTER TABLE public.dispatch_items          VALIDATE CONSTRAINT ck_dispatch_items_quantity;
ALTER TABLE public.subscription_plans      VALIDATE CONSTRAINT ck_subscription_plans_prices;
ALTER TABLE public.subscription_promos     VALIDATE CONSTRAINT ck_subscription_promos_discount;
ALTER TABLE public.market_ads              VALIDATE CONSTRAINT ck_market_ads_price;
