-- =============================================================================
-- GreenRoot V1 — Clean Master Schema
-- =============================================================================
-- Apply to a FRESH empty database:
--   psql "$DATABASE_URL" -f greenroot_schema.sql
--
-- No ALTER TABLE, no partial fragments, no migration ordering headaches.
-- Every table is created once with all columns inline.
-- One exception: quotations ↔ orders have a circular FK (quotations.converted_order_id
-- → orders, orders.quotation_id → quotations). One FK is added at the end — it is
-- the only ALTER TABLE in this file, and it is structurally unavoidable.
--
-- Tables: 49  |  Sections: DDL → Constraints → Indexes → Reference seed data
-- Gap fixes applied: otp_requests, platform_config, nursery_applications added;
--   nurseries/orders/dispatches/attachments got deleted_at; nurseries got
--   approval columns; dispatches got driver acceptance columns;
--   partial unique indexes enforce one-active-nursery and one-active-trip rules.
-- =============================================================================

SET statement_timeout = 0;
SET lock_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET client_min_messages = warning;
SET row_security = off;


-- =============================================================================
-- SECTION 1: ENUMS
-- =============================================================================

/*
 * gender_type
 * Used on the users table. Allows the mobile app to display a gender picker
 * during profile setup. Drivers and buyers both set this.
 */
CREATE TYPE public.gender_type AS ENUM (
    'MALE',
    'FEMALE',
    'NON_BINARY',
    'OTHER',
    'PREFER_NOT_TO_SAY'
);


-- =============================================================================
-- SECTION 2: SEQUENCE HELPER — must come before any table that uses it as DEFAULT
-- =============================================================================

/*
 * public_code_sequences
 * Central counter store for all human-readable public codes across the platform.
 * Each row tracks the last issued number for one entity type, optionally partitioned
 * by date (for date-based codes like ORD-20260622-0001).
 *
 * Who uses it: the application layer only — end users see the codes (USR-000001,
 * NUR-000001, ORD-20260622-0001) but never interact with this table directly.
 *
 * Why: internal bigint PKs are used for joins; public codes are stable, human-
 * readable identifiers for customer support, WhatsApp sharing, and QR codes.
 */
CREATE TABLE public.public_code_sequences (
    code_key    VARCHAR(40)  NOT NULL,
    date_key    VARCHAR(8)   NOT NULL DEFAULT '',
    last_value  BIGINT       NOT NULL DEFAULT 0,
    updated_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT public_code_sequences_pkey PRIMARY KEY (code_key, date_key)
);

/*
 * next_public_code()
 * Thread-safe atomic counter increment using INSERT … ON CONFLICT DO UPDATE.
 * Returns formatted code: PREFIX-NNNNNN or PREFIX-YYYYMMDD-NNNN.
 * Examples: 'USR-000001', 'ORD-20260622-0001'
 */
CREATE OR REPLACE FUNCTION public.next_public_code(
    p_code_key    TEXT,
    p_prefix      TEXT,
    p_width       INTEGER,
    p_date_based  BOOLEAN   DEFAULT false,
    p_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) RETURNS TEXT
LANGUAGE plpgsql AS $$
DECLARE
    v_date_key  TEXT := '';
    v_last_value BIGINT;
BEGIN
    IF p_date_based THEN
        v_date_key := to_char(p_at::DATE, 'YYYYMMDD');
    END IF;

    INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
    VALUES (p_code_key, v_date_key, 1)
    ON CONFLICT (code_key, date_key)
    DO UPDATE SET
        last_value = public.public_code_sequences.last_value + 1,
        updated_at = CURRENT_TIMESTAMP
    RETURNING last_value INTO v_last_value;

    IF p_date_based THEN
        RETURN p_prefix || '-' || v_date_key || '-' || lpad(v_last_value::TEXT, p_width, '0');
    END IF;

    RETURN p_prefix || '-' || lpad(v_last_value::TEXT, p_width, '0');
END;
$$;


-- =============================================================================
-- SECTION 3: REFERENCE / LOOKUP TABLES  (no foreign keys, seeded at startup)
-- =============================================================================

/*
 * roles
 * Platform-level roles that control what a user can do across the entire system.
 * Active roles: SUPER_ADMIN, ADMIN, NURSERY_OWNER, MANAGER, DRIVER, BUYER, CUSTOMER.
 * TRANSPORT_PROVIDER exists but is deactivated for V1.
 *
 * Who uses it: every user has at least one role assigned via user_roles.
 * Why: drives RBAC middleware — which API endpoints a user can call, what data
 * they can see, and which UI screens are shown to them.
 */
CREATE TABLE public.roles (
    role_id     SMALLSERIAL  PRIMARY KEY,
    role_code   VARCHAR(50)  NOT NULL UNIQUE,
    role_name   VARCHAR(100) NOT NULL,
    description TEXT,
    is_active   BOOLEAN      DEFAULT true,
    created_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

/*
 * nursery_roles
 * Fine-grained roles within a single nursery (OWNER, PARTNER, MANAGER, OPERATOR,
 * ACCOUNTANT, DISPATCHER). These are nursery-scoped, distinct from platform roles.
 *
 * Who uses it: nursery owners assign these to staff; stored in nursery_users.
 * Why: an OWNER can approve quotations and see financials; an OPERATOR can only
 * manage inventory; a DISPATCHER handles loading/dispatch workflow.
 * Note: in V1 the primary active nursery role is MANAGER (gumastha).
 */
CREATE TABLE public.nursery_roles (
    nursery_role_id  SMALLSERIAL  PRIMARY KEY,
    role_code        VARCHAR(50)  NOT NULL UNIQUE,
    role_name        VARCHAR(100) NOT NULL,
    description      TEXT,
    is_active        BOOLEAN      NOT NULL DEFAULT true,
    created_at       TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * languages
 * Supported UI languages for multilingual plant names and app content.
 * Examples: en (English), hi (Hindi), te (Telugu), ta (Tamil).
 *
 * Who uses it: admin adds languages; plant_names references language_id to
 * store the plant name in that language.
 * Why: nurseries across different Indian states use regional language names
 * for plants; buyers search by local name.
 */
CREATE TABLE public.languages (
    language_id    SMALLSERIAL  PRIMARY KEY,
    language_code  VARCHAR(10)  NOT NULL UNIQUE,
    language_name  VARCHAR(100) NOT NULL,
    is_active      BOOLEAN      NOT NULL DEFAULT true,
    created_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * plant_sizes
 * Standard size codes for plants: SEED, SAPLING, SMALL, MEDIUM, LARGE, EXTRA_LARGE.
 * Used on inventory, order items, and quotation items so pricing can vary by size.
 *
 * Who uses it: admin configures sizes; nursery owners price by size; buyers order
 * by size; the mobile app shows a size picker.
 * Why: a 5-inch Mango sapling and a 10-foot Mango tree are very different products;
 * pricing, packaging, and transport all depend on size.
 */
CREATE TABLE public.plant_sizes (
    size_id       SMALLSERIAL  PRIMARY KEY,
    size_code     VARCHAR(50)  NOT NULL UNIQUE,
    display_name  VARCHAR(100) NOT NULL,
    display_order SMALLINT     NOT NULL,
    is_active     BOOLEAN      NOT NULL DEFAULT true,
    created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * plant_categories
 * Taxonomy for plants: Fruit Trees, Medicinal Plants, Shade Trees, Herbs,
 * Ornamental, Flowering Shrubs, Indoor Plants, etc.
 *
 * Who uses it: admin creates categories; buyers filter the plant catalogue by
 * category in the mobile app; admin UI shows category-based reports.
 * Why: helps buyers quickly navigate a catalogue of hundreds of plants without
 * scrolling through an unsorted list.
 */
CREATE TABLE public.plant_categories (
    category_id    SERIAL       PRIMARY KEY,
    category_name  VARCHAR(100) NOT NULL UNIQUE,
    is_active      BOOLEAN      NOT NULL DEFAULT true,
    created_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * subscription_plans
 * Defines the SaaS subscription tiers offered to nurseries / owners (e.g. FREE,
 * STARTER, PRO). Controls max_users and max_nurseries limits per account.
 *
 * Who uses it: admin creates plans; nursery owners subscribe to a plan;
 * user_subscriptions records which owner is on which plan.
 * Why: GreenRoot earns recurring revenue from nursery owners; plans gate premium
 * features and usage limits.
 */
CREATE TABLE public.subscription_plans (
    plan_id         BIGSERIAL    PRIMARY KEY,
    plan_code       VARCHAR(50)  NOT NULL UNIQUE,
    plan_name       VARCHAR(100) NOT NULL,
    description     TEXT,
    monthly_price   NUMERIC(12,2),
    yearly_price    NUMERIC(12,2),
    max_users       INTEGER,
    max_nurseries   INTEGER,
    is_active       BOOLEAN      DEFAULT true,
    created_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

/*
 * notification_templates
 * Reusable message templates for SMS, push, and email notifications.
 * Each template has a code (e.g. ORDER_CONFIRMED), a channel (SMS/PUSH/EMAIL),
 * and a message body with {{variable}} placeholders.
 *
 * Who uses it: admin configures templates; the notifications module resolves
 * the correct template when triggering a notification event.
 * Why: centralising message copy in DB allows marketing/ops to edit messages
 * without a code deploy.
 */
CREATE TABLE public.notification_templates (
    template_id       BIGSERIAL    PRIMARY KEY,
    template_code     VARCHAR(100) NOT NULL UNIQUE,
    template_name     VARCHAR(255),
    channel           VARCHAR(30),
    subject           VARCHAR(255),
    message_template  TEXT,
    is_active         BOOLEAN      DEFAULT true,
    created_at        TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);


/*
 * platform_config
 * Runtime key-value configuration managed by Super Admin without a code deploy.
 * data_type hints how the API should parse the value (string/integer/numeric/boolean).
 *
 * Who uses it: Super Admin reads/writes via admin portal. API layer reads on
 * startup and caches; refreshes on change. Notification worker reads OTP settings.
 * Why: avoids hardcoding OTP expiry, fee percentages, or approval windows in Go
 * binary — ops can change them live without a deployment.
 */
CREATE TABLE public.platform_config (
    config_id    BIGSERIAL    PRIMARY KEY,
    config_key   VARCHAR(100) NOT NULL UNIQUE,
    config_value TEXT         NOT NULL,
    data_type    VARCHAR(20)  NOT NULL DEFAULT 'string',
    description  TEXT,
    is_active    BOOLEAN      NOT NULL DEFAULT true,
    updated_by   BIGINT,
    updated_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 4: USERS & IDENTITY
-- =============================================================================

/*
 * users
 * Every person who interacts with GreenRoot — buyers, nursery owners, managers,
 * drivers, and admins — has exactly one row here. Login is OTP-on-mobile;
 * password_hash is optional for accounts that enabled password login.
 *
 * Who uses it: everyone. Auth module reads/writes on login. Admin portal lists
 * users. Mobile app updates profile_image_url and gender.
 * Why: single source of identity; user_code (USR-000001) is the public identifier
 * shown in support tickets and audit logs.
 */
CREATE TABLE public.users (
    user_id            BIGSERIAL            PRIMARY KEY,
    user_code          VARCHAR(20)          NOT NULL UNIQUE
                           DEFAULT public.next_public_code('users', 'USR', 6, false),
    first_name         VARCHAR(100)         NOT NULL,
    last_name          VARCHAR(100),
    mobile             VARCHAR(20)          NOT NULL UNIQUE,
    email              VARCHAR(255)         UNIQUE,
    password_hash      TEXT,
    profile_image_url  TEXT,
    mobile_verified    BOOLEAN              DEFAULT false,
    email_verified     BOOLEAN              DEFAULT false,
    gender             public.gender_type,
    status             VARCHAR(20)          DEFAULT 'ACTIVE',
    last_login_at      TIMESTAMP,
    deleted_at         TIMESTAMP,
    created_at         TIMESTAMP            DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP            DEFAULT CURRENT_TIMESTAMP,
    created_by         BIGINT,
    updated_by         BIGINT
);

/*
 * user_roles
 * Maps a user to one or more platform roles. A single user can hold multiple
 * roles (e.g. NURSERY_OWNER + BUYER).
 *
 * Who uses it: auth middleware reads this on every request to build the JWT claim.
 * Admin portal assigns roles when inviting users.
 * Why: a nursery owner who also buys plants from other nurseries needs both
 * NURSERY_OWNER and BUYER roles to use all features.
 */
CREATE TABLE public.user_roles (
    user_id      BIGINT    NOT NULL,
    role_id      SMALLINT  NOT NULL,
    assigned_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    assigned_by  BIGINT,
    CONSTRAINT user_roles_pkey PRIMARY KEY (user_id, role_id)
);

/*
 * user_sessions
 * Tracks active login sessions. Each OTP login creates a session row that
 * records the device, OS, app version, and IP for security audit purposes.
 *
 * Who uses it: auth module creates/invalidates sessions on login/logout.
 * Security team audits suspicious IPs or multiple concurrent sessions.
 * Why: allows forced logout of all sessions, session expiry, and rate-limit
 * abuse detection per device.
 */
CREATE TABLE public.user_sessions (
    session_id       BIGSERIAL    PRIMARY KEY,
    user_id          BIGINT       NOT NULL,
    session_token    VARCHAR(255),
    login_time       TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_activity_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    session_status   VARCHAR(20)  DEFAULT 'ACTIVE',
    device_type      VARCHAR(50),
    os_name          VARCHAR(50),
    app_version      VARCHAR(50),
    ip_address       VARCHAR(100),
    user_agent       TEXT,
    created_at       TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

/*
 * otp_requests
 * Stores every OTP sent to a mobile number. Verified by the auth module on login.
 * attempt_count tracks wrong guesses so the code can be blocked after N failures.
 * is_used prevents replay attacks — a code is single-use.
 * purpose distinguishes LOGIN from VERIFY_EMAIL or RESET_PASSWORD flows.
 *
 * Who uses it: auth module creates a row on every OTP send, marks is_used=true
 * on successful verification. Rate-limit middleware reads attempt_count.
 * Why: without this table there is no place to store the OTP in production;
 * the DEV admin works with a hardcoded OTP but that must not reach production.
 */
CREATE TABLE public.otp_requests (
    otp_id        BIGSERIAL    PRIMARY KEY,
    mobile        VARCHAR(20)  NOT NULL,
    otp_code      VARCHAR(10)  NOT NULL,
    purpose       VARCHAR(50)  NOT NULL DEFAULT 'LOGIN',
    expires_at    TIMESTAMP    NOT NULL,
    is_used       BOOLEAN      NOT NULL DEFAULT false,
    used_at       TIMESTAMP,
    attempt_count INTEGER      NOT NULL DEFAULT 0,
    ip_address    VARCHAR(100),
    created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * user_activities
 * Append-only event log of what each user did: viewed a plant, placed an order,
 * updated inventory, etc. Linked to a session so you can trace a full session
 * timeline.
 *
 * Who uses it: analytics, support, and fraud detection. Not shown to end users.
 * Why: helps understand feature usage, debug issues ("what exactly did this user
 * click before the error?"), and detect abuse patterns.
 */
CREATE TABLE public.user_activities (
    activity_id        BIGSERIAL    PRIMARY KEY,
    user_id            BIGINT       NOT NULL,
    session_id         BIGINT,
    activity_type      VARCHAR(100) NOT NULL,
    entity_type        VARCHAR(100),
    entity_id          BIGINT,
    activity_data      JSONB,
    activity_timestamp TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * user_addresses
 * Saved delivery addresses for buyers. Multiple addresses per user (home, office,
 * farm). is_default marks the pre-selected address at checkout.
 *
 * Who uses it: buyers add/select addresses on the mobile app. The order creation
 * API pre-fills the delivery address from this table.
 * Why: buyers ordering regularly to the same farm shouldn't retype the address
 * every time; lat/lng enables map pin placement.
 */
CREATE TABLE public.user_addresses (
    address_id     BIGSERIAL    PRIMARY KEY,
    user_id        BIGINT       NOT NULL,
    address_type   VARCHAR(50),
    contact_name   VARCHAR(100),
    contact_mobile VARCHAR(20),
    address_line1  VARCHAR(255) NOT NULL,
    address_line2  VARCHAR(255),
    city           VARCHAR(100),
    state          VARCHAR(100),
    country        VARCHAR(100),
    postal_code    VARCHAR(20),
    latitude       NUMERIC(10,7),
    longitude      NUMERIC(10,7),
    is_default     BOOLEAN      DEFAULT false,
    created_at     TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

/*
 * user_subscriptions
 * Records which subscription plan a nursery owner is currently on, start/end
 * dates, and whether auto-renew is enabled.
 *
 * Who uses it: admin activates/cancels subscriptions. Billing module creates a
 * payment row when a subscription is renewed.
 * Why: ties an owner to a plan that controls their feature limits; subscription_code
 * (SUB-000001) is used on invoices and support tickets.
 */
CREATE TABLE public.user_subscriptions (
    user_subscription_id  BIGSERIAL   PRIMARY KEY,
    subscription_code     VARCHAR(20) NOT NULL UNIQUE
                              DEFAULT public.next_public_code('user_subscriptions', 'SUB', 6, false),
    user_id               BIGINT      NOT NULL,
    plan_id               BIGINT      NOT NULL,
    start_date            DATE        NOT NULL,
    end_date              DATE,
    subscription_status   VARCHAR(30) DEFAULT 'ACTIVE',
    auto_renew            BOOLEAN     DEFAULT false,
    created_at            TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMP   DEFAULT CURRENT_TIMESTAMP
);

/*
 * user_notification_devices
 * FCM (Firebase Cloud Messaging) push token per device. A user can have multiple
 * devices (phone + tablet). Tokens are refreshed by the mobile app on each login.
 *
 * Who uses it: mobile app registers the token on startup. Notification worker
 * queries active tokens for a user_id to fan-out push messages.
 * Why: without this table, push notifications cannot be delivered to a specific
 * user's phone(s).
 */
CREATE TABLE public.user_notification_devices (
    device_id          BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    user_id            BIGINT       NOT NULL,
    fcm_token          TEXT         NOT NULL UNIQUE,
    device_type        VARCHAR(50),
    device_id_external VARCHAR(255),
    platform           VARCHAR(50),
    app_version        VARCHAR(50),
    is_active          BOOLEAN      NOT NULL DEFAULT true,
    last_seen_at       TIMESTAMP,
    created_at         TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 5: NURSERY
-- =============================================================================

/*
 * nurseries
 * A nursery is the core selling entity on GreenRoot. Each nursery has one owner
 * (owner_user_id, UNIQUE), a GST number for invoicing, and a nursery_code
 * (NUR-000001) used in all communications.
 *
 * Who uses it: nursery owner creates/edits their nursery via the mobile app or
 * admin portal. Admin approves new nurseries. Buyers see nursery details on order.
 * Why: the platform is nursery-centric — inventory, quotations, orders, dispatches,
 * and managers all belong to a nursery.
 */
CREATE TABLE public.nurseries (
    nursery_id       BIGSERIAL    PRIMARY KEY,
    nursery_code     VARCHAR(50)  NOT NULL UNIQUE
                         DEFAULT public.next_public_code('nurseries', 'NUR', 6, false),
    nursery_name     VARCHAR(255) NOT NULL,
    owner_user_id    BIGINT       UNIQUE,
    gst_number       VARCHAR(50),
    mobile           VARCHAR(20),
    email            VARCHAR(255),
    website          VARCHAR(255),
    description      TEXT,
    status           VARCHAR(20)  DEFAULT 'PENDING_APPROVAL',  -- PENDING_APPROVAL → ACTIVE | REJECTED | SUSPENDED
    approved_by      BIGINT,        -- super admin who approved
    approved_at      TIMESTAMP,
    rejected_by      BIGINT,        -- super admin who rejected
    rejected_at      TIMESTAMP,
    rejection_reason TEXT,
    deleted_at       TIMESTAMP,     -- soft delete (business rule: never hard-delete nurseries)
    created_at       TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by       BIGINT,
    updated_by       BIGINT
);

/*
 * nursery_applications
 * Pre-approval onboarding record submitted by a prospective nursery owner.
 * Super Admin reviews PENDING applications and either approves (creates the
 * nursery row, links nursery_id here) or rejects with a reason.
 * Status: PENDING → APPROVED | REJECTED.
 *
 * Who uses it: nursery owner submits during registration. Super Admin reviews
 * in the admin portal "Nursery Applications" page. On approval, the system
 * creates the nurseries row and sets nursery_id here for traceability.
 * Why: without this table a nursery goes live immediately (status defaulted
 * to ACTIVE) bypassing the mandatory Super Admin approval step.
 */
CREATE TABLE public.nursery_applications (
    application_id    BIGSERIAL    PRIMARY KEY,
    application_code  VARCHAR(20)  NOT NULL UNIQUE
                          DEFAULT public.next_public_code('nursery_applications', 'NRA', 6, false),
    applicant_user_id BIGINT       NOT NULL,
    nursery_name      VARCHAR(255) NOT NULL,
    gst_number        VARCHAR(50),
    mobile            VARCHAR(20),
    email             VARCHAR(255),
    address_line1     VARCHAR(255),
    address_line2     VARCHAR(255),
    city              VARCHAR(100),
    state             VARCHAR(100),
    country           VARCHAR(100) DEFAULT 'India',
    postal_code       VARCHAR(20),
    description       TEXT,
    status            VARCHAR(30)  NOT NULL DEFAULT 'PENDING',
    reviewed_by       BIGINT,
    reviewed_at       TIMESTAMP,
    rejection_reason  TEXT,
    nursery_id        BIGINT,       -- set when application is approved and nursery row created
    created_at        TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * nursery_addresses
 * Physical location(s) of a nursery. A nursery can have multiple addresses
 * (farm gate, registered office). is_primary marks the main address shown
 * on quotations and maps.
 *
 * Who uses it: nursery owner adds addresses during onboarding. Buyers see the
 * primary address on the order page. Drivers use lat/lng for navigation.
 * Why: nurseries in India often have a separate farm address and a registered
 * business address; both matter for logistics and GST compliance.
 */
CREATE TABLE public.nursery_addresses (
    nursery_address_id  BIGSERIAL    PRIMARY KEY,
    nursery_id          BIGINT       NOT NULL,
    address_type        VARCHAR(50),
    address_line1       VARCHAR(255),
    address_line2       VARCHAR(255),
    city                VARCHAR(100),
    state               VARCHAR(100),
    country             VARCHAR(100),
    postal_code         VARCHAR(20),
    latitude            NUMERIC(10,7),
    longitude           NUMERIC(10,7),
    is_primary          BOOLEAN      DEFAULT false,
    created_at          TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

/*
 * nursery_users
 * Staff members (managers / gumastha) assigned to a nursery. A manager has a
 * text role (MANAGER or GUMASTHA) and a status (ACTIVE/INACTIVE). The legacy
 * nursery_role_id FK is kept nullable for backward compatibility.
 *
 * Who uses it: nursery owner invites managers via QR code / link. Managers use
 * the mobile app to manage inventory, quotations, and loading.
 * Why: an owner cannot be present at the nursery every day; managers handle
 * day-to-day operations on their behalf. V1 rule: a manager cannot simultaneously
 * be a nursery owner.
 */
CREATE TABLE public.nursery_users (
    nursery_user_id     BIGSERIAL   PRIMARY KEY,
    nursery_id          BIGINT      NOT NULL,
    user_id             BIGINT      NOT NULL,
    role                VARCHAR(50) NOT NULL DEFAULT 'MANAGER',
    status              VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    nursery_role_id     SMALLINT,
    invited_by_user_id  BIGINT,
    joined_at           TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_active           BOOLEAN     DEFAULT true,
    CONSTRAINT nursery_users_uq UNIQUE (nursery_id, user_id, nursery_role_id)
    -- Note: partial unique index uq_manager_one_active_nursery enforces
    -- "one active nursery per manager" business rule — added in SECTION 17.
);

/*
 * nursery_drivers
 * Many-to-many connection between independent drivers and nurseries. A driver
 * can service multiple nurseries; a nursery can work with multiple drivers.
 * Connection goes through REQUESTED → APPROVED states.
 *
 * Who uses it: nursery manager invites a driver by sharing a QR code with a
 * UUID. Driver scans it and requests connection. Manager approves.
 * Why: V1 rule — drivers are independent workers, not owned by any nursery.
 * This table is the bridge that lets a nursery assign an approved driver to a
 * dispatch without employing them permanently.
 */
CREATE TABLE public.nursery_drivers (
    id                   BIGSERIAL   PRIMARY KEY,
    nursery_id           BIGINT      NOT NULL,
    driver_user_id       BIGINT      NOT NULL,
    invited_by_user_id   BIGINT,
    approved_by_user_id  BIGINT,
    connection_status    VARCHAR(20) NOT NULL DEFAULT 'REQUESTED',
    connected_at         TIMESTAMP,
    created_at           TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_nursery_driver UNIQUE (nursery_id, driver_user_id)
);

/*
 * nursery_inventory
 * The stock book of a nursery — how many units of plant X in size Y are currently
 * available. inventory_status can be AVAILABLE, LOW_STOCK, or OUT_OF_STOCK.
 * One row per (nursery, plant, size) combination.
 *
 * Who uses it: manager updates quantities after receiving stock or making a sale.
 * Buyer sees live stock when browsing. Order creation checks availability.
 * Why: without inventory tracking, a nursery cannot commit to an order or price
 * a quotation accurately; inventory_code (INV-000001) appears on pick lists.
 */
CREATE TABLE public.nursery_inventory (
    inventory_id      BIGSERIAL    PRIMARY KEY,
    inventory_code    VARCHAR(20)  NOT NULL UNIQUE
                          DEFAULT public.next_public_code('nursery_inventory', 'INV', 6, false),
    nursery_id        BIGINT       NOT NULL,
    plant_id          BIGINT       NOT NULL,
    size_id           SMALLINT     NOT NULL,
    available_quantity INTEGER     NOT NULL DEFAULT 0,
    inventory_status  VARCHAR(20)  NOT NULL DEFAULT 'AVAILABLE',
    last_updated_by   BIGINT,
    last_updated_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at        TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_inventory UNIQUE (nursery_id, plant_id, size_id)
);


-- =============================================================================
-- SECTION 6: PLANT CATALOGUE
-- =============================================================================

/*
 * plants
 * Master catalogue of every plant species on the platform. One row per species —
 * shared across all nurseries. Nurseries maintain their own stock via
 * nursery_inventory; names in regional languages via plant_names.
 *
 * Who uses it: admin adds plants. All roles search/browse plants. Nursery owners
 * add plants to their inventory. Buyers select plants when creating orders.
 * Why: a shared catalogue ensures consistent scientific names, prevents duplicate
 * plant records, and enables cross-nursery price comparisons.
 */
CREATE TABLE public.plants (
    plant_id            BIGSERIAL    PRIMARY KEY,
    plant_code          VARCHAR(20)  NOT NULL UNIQUE
                            DEFAULT public.next_public_code('plants', 'PLT', 6, false),
    scientific_name     VARCHAR(255) NOT NULL UNIQUE,
    common_name         VARCHAR(255),
    plant_type          VARCHAR(50),
    light_requirement   VARCHAR(50),
    water_requirement   VARCHAR(50),
    english_description TEXT,
    is_active           BOOLEAN      NOT NULL DEFAULT true,
    created_at          TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * plant_names
 * Stores the plant name in each supported language. One row per (plant, language).
 * Used for multilingual search and display in the mobile app.
 *
 * Who uses it: admin adds translations after adding a new plant. Mobile app
 * shows the name in the buyer's preferred language.
 * Why: Tamil-speaking buyers in Tamil Nadu search "வேம்பு" not "Azadirachta indica";
 * regional names drive discovery and trust.
 */
CREATE TABLE public.plant_names (
    plant_name_id  BIGSERIAL    PRIMARY KEY,
    plant_id       BIGINT       NOT NULL,
    language_id    SMALLINT     NOT NULL,
    plant_name     VARCHAR(255) NOT NULL,
    description    TEXT,
    is_active      BOOLEAN      NOT NULL DEFAULT true,
    created_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_plant_language UNIQUE (plant_id, language_id)
);

/*
 * plant_category_mapping
 * Many-to-many: a plant can belong to multiple categories
 * (e.g. Neem is both Medicinal Plants and Shade Trees).
 *
 * Who uses it: admin maps plants to categories. Buyer filter in the app reads
 * this to show "Fruit Trees" results.
 * Why: category-based browsing is the primary discovery path for buyers who
 * don't know the plant's scientific name.
 */
CREATE TABLE public.plant_category_mapping (
    plant_id     BIGINT    NOT NULL,
    category_id  INTEGER   NOT NULL,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT plant_category_mapping_pkey PRIMARY KEY (plant_id, category_id)
);

/*
 * plant_images
 * Photos of a plant uploaded to MinIO/S3. is_primary marks the thumbnail shown
 * in listings; display_order controls the gallery sequence.
 *
 * Who uses it: admin / nursery owner uploads images via the presign endpoint.
 * Mobile app displays gallery. Quotation PDF renders the primary image.
 * Why: buyers making Rs 50,000+ plant purchases need to see what they are buying;
 * images significantly increase order conversion.
 */
CREATE TABLE public.plant_images (
    image_id      BIGSERIAL    PRIMARY KEY,
    plant_id      BIGINT       NOT NULL,
    image_url     TEXT         NOT NULL,
    alt_text      VARCHAR(255),
    display_order INTEGER      NOT NULL DEFAULT 0,
    is_primary    BOOLEAN      NOT NULL DEFAULT false,
    created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * plant_care_guides
 * One care guide per plant. Contains structured sections: sunlight, watering,
 * soil, temperature, fertilizer, pruning. Displayed in the mobile app on the
 * plant detail page.
 *
 * Who uses it: admin writes care guides once. Buyers and nursery staff read them.
 * Why: value-add content that differentiates GreenRoot from a plain catalogue;
 * reduces after-sale support calls about why plants are dying.
 */
CREATE TABLE public.plant_care_guides (
    care_guide_id  BIGSERIAL  PRIMARY KEY,
    plant_id       BIGINT     NOT NULL UNIQUE,
    sunlight       TEXT,
    watering       TEXT,
    soil           TEXT,
    temperature    TEXT,
    fertilizer     TEXT,
    pruning        TEXT,
    notes          TEXT,
    created_at     TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * plant_requests
 * Inter-nursery sourcing requests. When a nursery is short of a plant, it
 * broadcasts a request to nearby nurseries who can respond with availability.
 * Status: OPEN → (suppliers respond) → CLOSED or REJECTED.
 *
 * Who uses it: nursery manager creates a request. Other nursery managers see
 * open requests and respond via plant_request_responses.
 * Why: enables nurseries to source plants from each other rather than rejecting
 * a buyer order; creates a B2B marketplace layer within the platform.
 */
CREATE TABLE public.plant_requests (
    request_id           BIGSERIAL    PRIMARY KEY,
    request_code         VARCHAR(30)  NOT NULL UNIQUE
                             DEFAULT public.next_public_code('plant_requests', 'REQ', 4, true),
    requesting_nursery_id BIGINT      NOT NULL,
    requested_by_user_id  BIGINT      NOT NULL,
    plant_id             BIGINT       NOT NULL,
    size_id              SMALLINT,
    quantity_required    INTEGER      NOT NULL,
    required_by_date     DATE,
    radius_km            INTEGER      NOT NULL DEFAULT 50,
    notes                TEXT,
    status               VARCHAR(30)  NOT NULL DEFAULT 'OPEN',
    expires_at           TIMESTAMP,
    fulfilled_at         TIMESTAMP,
    created_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * plant_request_responses
 * Responses from supplier nurseries to an open plant request. Each response
 * states how many units are available and at what status.
 * Statuses: AVAILABLE → ACCEPTED / REJECTED.
 *
 * Who uses it: supplier nursery manager responds to requests they can fulfil.
 * Requesting nursery manager reviews responses and selects one.
 * Why: closes the sourcing loop — the requesting nursery can compare multiple
 * supplier responses and place an order with the best one.
 */
CREATE TABLE public.plant_request_responses (
    response_id           BIGSERIAL   PRIMARY KEY,
    request_id            BIGINT      NOT NULL,
    supplier_nursery_id   BIGINT      NOT NULL,
    responded_by_user_id  BIGINT      NOT NULL,
    available_quantity    INTEGER     NOT NULL,
    remarks               TEXT,
    status                VARCHAR(30) NOT NULL DEFAULT 'RESPONDED',
    created_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_request_supplier UNIQUE (request_id, supplier_nursery_id)
);


-- =============================================================================
-- SECTION 7: QUOTATIONS
-- Note: quotations.converted_order_id → orders FK is added AFTER orders is created.
-- =============================================================================

/*
 * quotations
 * A price estimate that a manager or owner creates for a customer or for internal
 * planning. Statuses: DRAFT → SENT → CUSTOMER_ACCEPTED / CUSTOMER_REJECTED →
 * CONVERTED (when turned into an order).
 *
 * Who uses it: nursery manager creates quotations for walk-in or WhatsApp
 * inquiries. Owner reviews and approves. Customer receives a PDF.
 * Why: most B2B plant sales start with a quote; converting a quote directly to
 * an order saves re-entry and maintains price traceability.
 *
 * Fields:
 *   nursery_id / nursery_name / nursery_phone — nursery snapshot at time of quote
 *   customer_user_id — linked user if they are on the platform
 *   customer_name / customer_mobile — free-text customer info (not all buyers have accounts)
 *   assigned_manager_user_id — manager responsible for this quote
 *   converted_order_id — set when quote is converted to an order
 *   deleted_at — soft delete
 */
CREATE TABLE public.quotations (
    quotation_id             BIGSERIAL    PRIMARY KEY,
    quotation_code           VARCHAR(30)  NOT NULL UNIQUE,
    created_by_user_id       BIGINT       NOT NULL,
    created_by_name          VARCHAR(255),
    nursery_id               BIGINT,
    nursery_name             VARCHAR(255),
    nursery_phone            VARCHAR(20),
    customer_user_id         BIGINT,
    assigned_manager_user_id BIGINT,
    customer_name            VARCHAR(255),
    customer_mobile          VARCHAR(20),
    recipient_name           VARCHAR(255),
    recipient_mobile         VARCHAR(20),
    buyer_nursery_id         BIGINT,
    notes                    TEXT,
    total_amount             NUMERIC(12,2) NOT NULL DEFAULT 0,
    status                   VARCHAR(20)  NOT NULL DEFAULT 'DRAFT',
    converted_order_id       BIGINT,       -- FK to orders added after orders table
    converted_by_user_id     BIGINT,
    converted_at             TIMESTAMP,
    deleted_at               TIMESTAMP,
    created_at               TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * quotation_items
 * Line items on a quotation. Each row is one plant + size + quantity + price.
 * plant_name_snapshot stores the name at time of quoting in case the plant
 * is renamed or deleted later.
 *
 * Who uses it: manager adds items when building a quotation. PDF generator
 * reads these to produce the quotation document.
 * Why: prices can change; snapshotting name and price at quote time ensures
 * the PDF matches what was agreed even months later.
 */
CREATE TABLE public.quotation_items (
    quotation_item_id    BIGSERIAL    PRIMARY KEY,
    quotation_id         BIGINT       NOT NULL,
    plant_id             BIGINT       NOT NULL,
    scientific_name      VARCHAR(255) NOT NULL,
    common_name          VARCHAR(255),
    plant_name_snapshot  VARCHAR(255),
    description          TEXT,
    size                 VARCHAR(100),
    quantity             NUMERIC(12,2) NOT NULL,
    unit_price           NUMERIC(12,2),
    total_price          NUMERIC(12,2),
    remarks              TEXT,
    created_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 8: ORDERS
-- =============================================================================

/*
 * orders
 * A confirmed sale from a nursery to a customer. Created either by converting a
 * quotation or directly. Goes through:
 *   DRAFT → CONFIRMED → LOADING → DISPATCHED → DELIVERED
 *   (or CANCELLED at any point before DISPATCHED)
 *
 * Who uses it: manager / owner creates orders. Buyer tracks order status in the
 * mobile app. Admin monitors order pipeline in the admin portal.
 * Why: the central transaction record — inventory is reserved, payments are
 * linked, and dispatches are created against an order.
 *
 * Fields:
 *   order_code — public code (ORD-20260622-0001) shown to customer
 *   nursery_id — primary nursery fulfilling this order
 *   buyer_nursery_id — if buyer is another nursery (B2B trade)
 *   loading_started_at / loading_completed_at — timestamp the loading workflow
 *   cancel fields — who cancelled and why (audit trail)
 */
CREATE TABLE public.orders (
    order_id                      BIGSERIAL    PRIMARY KEY,
    order_code                    VARCHAR(30)  NOT NULL UNIQUE
                                      DEFAULT public.next_public_code('orders', 'ORD', 4, true),
    order_number                  VARCHAR(50)  NOT NULL UNIQUE,
    nursery_id                    BIGINT,
    seller_nursery_id             BIGINT,
    buyer_nursery_id              BIGINT,
    quotation_id                  BIGINT,
    buyer_user_id                 BIGINT,
    customer_user_id              BIGINT,
    customer_name                 VARCHAR(255),
    customer_mobile               VARCHAR(20),
    assigned_manager_user_id      BIGINT,
    created_by_user_id            BIGINT,
    order_status                  VARCHAR(30)  NOT NULL DEFAULT 'PENDING',
    total_amount                  NUMERIC(15,2) DEFAULT 0,
    notes                         TEXT,
    order_date                    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    loading_started_at            TIMESTAMP,
    loading_completed_at          TIMESTAMP,
    loading_completed_by_user_id  BIGINT,
    cancelled_by_user_id          BIGINT,
    cancelled_at                  TIMESTAMP,
    cancel_reason                 TEXT,
    deleted_at                    TIMESTAMP,   -- soft delete (business rule: never hard-delete orders)
    created_at                    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by                    BIGINT,
    updated_by                    BIGINT
);

/*
 * order_items
 * Individual plant line items within an order. plant_name_snapshot and size
 * preserve what was ordered even if the plant catalogue changes later.
 * Pricing is optional in V1 (accounting module not yet live).
 *
 * Who uses it: manager adds/edits items when creating an order from scratch.
 * Loading staff reads items to prepare the consignment.
 * Why: the pick list for loading is built from order_items; each item becomes
 * one dispatch_item when the consignment is prepared for shipping.
 */
CREATE TABLE public.order_items (
    order_item_id        BIGSERIAL    PRIMARY KEY,
    order_id             BIGINT       NOT NULL,
    plant_id             BIGINT,
    plant_name_snapshot  VARCHAR(255),
    size_id              SMALLINT,
    size                 VARCHAR(100),
    quantity             NUMERIC(12,2) NOT NULL,
    unit_price           NUMERIC(15,2),
    total_price          NUMERIC(15,2),
    remarks              TEXT,
    created_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 9: DISPATCH & DELIVERY
-- =============================================================================

/*
 * dispatches
 * A delivery trip for one order. Contains vehicle and driver assignment,
 * customer delivery address, and the full lifecycle of the trip.
 * Status: CREATED → TRIP_STARTED → DELIVERED (or CANCELLED).
 *
 * Who uses it: manager creates a dispatch after loading is complete. Driver
 * starts the trip from the mobile app. Customer tracks in real-time via a
 * trip_tracking_links UUID. Admin monitors all active dispatches.
 * Why: separates the "order" (what was sold) from the "delivery" (how it
 * reaches the customer); one order can in theory have multiple dispatch legs.
 *
 * Snapshot fields (*_snapshot): captured at dispatch time so trip history
 * remains accurate even if user data changes later.
 */
CREATE TABLE public.dispatches (
    dispatch_id                  BIGSERIAL    PRIMARY KEY,
    dispatch_code                VARCHAR(30)  NOT NULL UNIQUE
                                     DEFAULT public.next_public_code('dispatches', 'DSP', 4, true),
    dispatch_number              VARCHAR(50)  UNIQUE,
    order_id                     BIGINT       NOT NULL,
    nursery_id                   BIGINT,
    dispatch_status              VARCHAR(30)  DEFAULT 'PENDING',
    dispatched_by                BIGINT,
    assigned_manager_user_id     BIGINT,
    vehicle_id                   BIGINT,
    driver_id                    BIGINT,
    driver_user_id               BIGINT,
    owner_user_id_snapshot       BIGINT,
    customer_user_id             BIGINT,
    customer_name_snapshot       VARCHAR(255),
    customer_mobile_snapshot     VARCHAR(20),
    destination_address          TEXT,
    dispatch_date                TIMESTAMP,
    delivery_date                TIMESTAMP,
    trip_started_at              TIMESTAMP,
    trip_started_by_user_id      BIGINT,
    completed_at                 TIMESTAMP,
    driver_accepted_at           TIMESTAMP,   -- when driver explicitly accepted the trip (Assigned → Accepted)
    driver_rejected_at           TIMESTAMP,
    driver_reject_reason         TEXT,
    deleted_at                   TIMESTAMP,   -- soft delete (business rule: never hard-delete dispatches)
    notes                        TEXT,
    created_at                   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
    -- Note: partial unique index uq_driver_one_active_trip enforces
    -- "driver can only have one active trip at a time" — added in SECTION 17.
);

/*
 * dispatch_items
 * The actual plant items loaded onto a specific dispatch. Linked to order_items;
 * quantity can be less than ordered if only a partial shipment is sent.
 *
 * Who uses it: loading staff records what was physically loaded per dispatch.
 * Manager reviews dispatch_items vs order_items to track partial fulfilment.
 * Why: an order for 100 trees might be dispatched in two trips of 50 each;
 * dispatch_items tracks exactly what was on each trip.
 */
CREATE TABLE public.dispatch_items (
    dispatch_item_id  BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    dispatch_id       BIGINT        NOT NULL,
    order_item_id     BIGINT,
    quantity          NUMERIC(12,2) NOT NULL DEFAULT 0,
    notes             TEXT,
    created_at        TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * dispatch_assignments
 * Links a dispatch to a specific vehicle and driver. Kept as a separate table
 * to support multi-vehicle or driver reassignment scenarios.
 * Status: ASSIGNED → COMPLETED.
 *
 * Who uses it: manager assigns vehicle and driver before releasing the dispatch.
 * Why: having a separate assignment table allows reassigning the driver without
 * modifying the dispatch row, and preserves the original assignment in history.
 */
CREATE TABLE public.dispatch_assignments (
    dispatch_assignment_id  BIGSERIAL   PRIMARY KEY,
    dispatch_id             BIGINT      NOT NULL,
    vehicle_id              BIGINT      NOT NULL,
    driver_id               BIGINT      NOT NULL,
    assigned_at             TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    assigned_by             BIGINT,
    status                  VARCHAR(20) DEFAULT 'ASSIGNED'
);

/*
 * trip_events
 * Chronological log of what happened during a delivery trip: TRIP_STARTED,
 * ARRIVED_AT_DELIVERY, PHOTO_TAKEN, DELIVERED, etc.
 * Optionally includes GPS coordinates and a photo URL for each event.
 *
 * Who uses it: driver logs events from the mobile app. Manager and customer
 * can view the event timeline on the dispatch detail page.
 * Why: creates an auditable delivery proof trail; photo_url of the delivered
 * plants at the customer location resolves most "I didn't receive it" disputes.
 */
CREATE TABLE public.trip_events (
    id                  BIGSERIAL    PRIMARY KEY,
    dispatch_id         BIGINT       NOT NULL,
    event_type          VARCHAR(50)  NOT NULL,
    latitude            NUMERIC(10,7),
    longitude           NUMERIC(10,7),
    photo_url           TEXT,
    remarks             TEXT,
    created_by_user_id  BIGINT,
    created_at          TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * trip_tracking_links
 * Generates a public UUID link (no login required) that a customer can open
 * to see the live driver location and trip status for their delivery.
 * The link expires after delivery or a set time.
 *
 * Who uses it: manager sends the tracking URL to the customer via WhatsApp/SMS
 * when the driver departs. Customer opens it in a browser.
 * Why: buyers in India expect to track their valuable plant order just like they
 * track food delivery; no app install required.
 */
CREATE TABLE public.trip_tracking_links (
    id                BIGSERIAL   PRIMARY KEY,
    tracking_uuid     UUID        NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    dispatch_id       BIGINT      NOT NULL,
    customer_user_id  BIGINT,
    customer_mobile   VARCHAR(20),
    expires_at        TIMESTAMP,
    status            VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    created_at        TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 10: VEHICLES & DRIVERS
-- =============================================================================

/*
 * vehicles
 * Registry of vehicles used for plant delivery. Each vehicle has a registration
 * number, type (TRUCK/TEMPO/PICKUP), and payload capacity in kg.
 * vehicle_code (VEH-000001) is used on loading slips.
 *
 * Who uses it: admin / owner registers vehicles. Manager assigns a vehicle to
 * a dispatch. Tracking module logs vehicle GPS.
 * Why: matching the right vehicle to an order by capacity prevents overloading;
 * vehicle history helps with maintenance planning.
 */
CREATE TABLE public.vehicles (
    vehicle_id      BIGSERIAL    PRIMARY KEY,
    vehicle_code    VARCHAR(20)  NOT NULL UNIQUE
                        DEFAULT public.next_public_code('vehicles', 'VEH', 6, false),
    vehicle_number  VARCHAR(50)  NOT NULL UNIQUE,
    vehicle_type    VARCHAR(50),
    capacity_kg     NUMERIC(12,2),
    owner_name      VARCHAR(255),
    mobile          VARCHAR(20),
    status          VARCHAR(20)  DEFAULT 'ACTIVE',
    created_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

/*
 * drivers
 * Driver profile — independent workers who can service any nursery they are
 * connected to via nursery_drivers. Includes licence details, profile completion
 * status, and admin approval status.
 *
 * Who uses it: driver registers via mobile app (uploads licence photo). Admin
 * reviews and approves (approval_status: PENDING → APPROVED). Nursery manager
 * then invites the approved driver.
 * Why: V1 business rule: drivers are freelancers, not employees of a nursery;
 * admin vetting ensures only licensed drivers enter the platform.
 */
CREATE TABLE public.drivers (
    driver_id             BIGSERIAL    PRIMARY KEY,
    driver_code           VARCHAR(20)  NOT NULL UNIQUE
                              DEFAULT public.next_public_code('drivers', 'DRV', 6, false),
    user_id               BIGINT       UNIQUE,
    license_number        VARCHAR(100),
    license_expiry_date   DATE,
    licence_photo_url     TEXT,
    vehicle_number        VARCHAR(50),
    vehicle_type          VARCHAR(50),
    emergency_contact     VARCHAR(20),
    profile_status        VARCHAR(20)  NOT NULL DEFAULT 'INCOMPLETE',
    approval_status       VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    approved_by_user_id   BIGINT,
    approved_at           TIMESTAMP,
    status                VARCHAR(20)  DEFAULT 'ACTIVE',
    created_at            TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

/*
 * driver_locations
 * GPS breadcrumb trail for a driver's movement. Created by the mobile app
 * every N seconds while a trip is active.
 *
 * Who uses it: live tracking map in admin portal and in customer's tracking page.
 * Driver app posts location silently in the background.
 * Why: enables real-time ETA calculation and post-delivery route replay for
 * disputes.
 */
CREATE TABLE public.driver_locations (
    location_id  BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    driver_id    BIGINT        NOT NULL,
    latitude     NUMERIC(10,7) NOT NULL,
    longitude    NUMERIC(10,7) NOT NULL,
    recorded_at  TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by   BIGINT
);

/*
 * vehicle_locations
 * GPS breadcrumb trail for a vehicle. Separate from driver_locations because
 * some vehicles may have a GPS device rather than relying on a driver's phone.
 * Includes speed and heading for richer telemetry.
 *
 * Who uses it: telematics integrations or IoT GPS devices post here. Admin
 * fleet map reads latest location per vehicle.
 * Why: driver phone GPS can fail or be switched off; having vehicle-level GPS
 * provides a backup and supports future IoT integration.
 */
CREATE TABLE public.vehicle_locations (
    location_id      BIGSERIAL    PRIMARY KEY,
    vehicle_id       BIGINT       NOT NULL,
    latitude         NUMERIC(10,7) NOT NULL,
    longitude        NUMERIC(10,7) NOT NULL,
    speed_kmph       NUMERIC(8,2),
    heading_degrees  NUMERIC(8,2),
    recorded_at      TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

/*
 * vehicle_tracking
 * Dispatch-linked GPS tracking — ties a vehicle + driver position to a specific
 * dispatch in progress. Used to build the live tracking view for customers.
 *
 * Who uses it: driver app posts here while a dispatch is TRIP_STARTED. Customer
 * tracking page polls this via the trip_tracking_links UUID.
 * Why: dispatch-linked tracking is what powers the "where is my delivery?" feature;
 * raw vehicle_locations don't carry the dispatch context needed to show the
 * right order status alongside the map.
 */
CREATE TABLE public.vehicle_tracking (
    tracking_id  BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    vehicle_id   BIGINT,
    driver_id    BIGINT,
    dispatch_id  BIGINT,
    latitude     NUMERIC(10,7) NOT NULL,
    longitude    NUMERIC(10,7) NOT NULL,
    tracked_at   TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    notes        TEXT
);


-- =============================================================================
-- SECTION 11: INVITES
-- =============================================================================

/*
 * invites
 * UUID-based invite tokens for onboarding new managers, drivers, or buyers.
 * A unique UUID is generated per invite; the recipient receives a deep link
 * with the UUID and completes registration.
 * invite_type: MANAGER | DRIVER | BUYER
 * status: PENDING → ACCEPTED | EXPIRED | REVOKED
 *
 * Who uses it: nursery owner invites a manager (MANAGER invite). Manager invites
 * a driver (DRIVER invite). Either shares the link via WhatsApp.
 * Why: prevents unauthorized accounts; ensures the right person is linked to
 * the right nursery during onboarding without manual admin intervention.
 */
CREATE TABLE public.invites (
    id                   BIGSERIAL    PRIMARY KEY,
    invite_uuid          UUID         NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    invite_type          VARCHAR(50)  NOT NULL,
    invited_by_user_id   BIGINT,
    nursery_id           BIGINT,
    role                 VARCHAR(50),
    target_mobile        VARCHAR(20),
    target_email         VARCHAR(255),
    target_name          VARCHAR(255),
    status               VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    expires_at           TIMESTAMP,
    accepted_by_user_id  BIGINT,
    accepted_at          TIMESTAMP,
    created_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 12: PAYMENTS
-- =============================================================================

/*
 * payments
 * Records every payment transaction — for an order or for a subscription renewal.
 * Supports Razorpay as the payment gateway (provider fields). Also supports
 * CASH/BANK_TRANSFER for offline payments logged manually.
 *
 * Who uses it: buyer pays via the mobile app (Razorpay checkout). Manager records
 * a cash payment manually. Admin reconciles payments in the portal.
 * Why: revenue tracking and order confirmation depend on knowing whether an
 * order has been paid; payment_code (PAY-20260622-0001) appears on invoices.
 */
CREATE TABLE public.payments (
    payment_id              BIGSERIAL    PRIMARY KEY,
    payment_code            VARCHAR(30)  NOT NULL UNIQUE
                                DEFAULT public.next_public_code('payments', 'PAY', 4, true),
    order_id                BIGINT,
    user_subscription_id    BIGINT,
    payment_for             VARCHAR(30)  NOT NULL DEFAULT 'ORDER',
    payer_user_id           BIGINT,
    amount                  NUMERIC(15,2) NOT NULL,
    payment_method          VARCHAR(50),
    payment_status          VARCHAR(30)  NOT NULL DEFAULT 'PENDING',
    payment_date            TIMESTAMP,
    provider                VARCHAR(50),
    provider_payment_id     VARCHAR(255),
    provider_order_id       VARCHAR(255),
    provider_signature      TEXT,
    transaction_reference   VARCHAR(255),
    raw_response            JSONB,
    notes                   TEXT,
    created_at              TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 13: NOTIFICATIONS
-- =============================================================================

/*
 * notifications
 * Outbound notification records — one row per message sent to a user.
 * notification_type classifies the message (ORDER_UPDATE, INVITE, SYSTEM, etc.).
 * data (JSONB) carries template variables like order_code, driver_name, etc.
 *
 * Who uses it: notification worker inserts a row after sending. Mobile app
 * queries unread notifications for the bell badge count.
 * Why: provides in-app notification history; allows resending failed notifications;
 * notification_code (NTF-000001) appears in support requests.
 */
CREATE TABLE public.notifications (
    notification_id      BIGSERIAL    PRIMARY KEY,
    notification_code    VARCHAR(20)  NOT NULL UNIQUE
                             DEFAULT public.next_public_code('notifications', 'NTF', 6, false),
    user_id              BIGINT,
    template_id          BIGINT,
    notification_type    VARCHAR(100) NOT NULL DEFAULT 'SYSTEM',
    title                VARCHAR(255),
    message              TEXT,
    channel              VARCHAR(30),
    data                 JSONB,
    notification_status  VARCHAR(30)  DEFAULT 'PENDING',
    sent_at              TIMESTAMP,
    read_at              TIMESTAMP,
    created_at           TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 14: ATTACHMENTS & AUDIT
-- =============================================================================

/*
 * attachments
 * Generic file attachment store. Any entity (order, nursery, driver, etc.)
 * can have files attached. entity_type + entity_id form the polymorphic key.
 * file_url points to MinIO/S3; file uploaded via the presign endpoint.
 *
 * Who uses it: manager attaches a purchase order PDF to an order. Driver
 * attaches a delivery proof photo. Admin attaches KYC documents to a nursery.
 * Why: a single attachments table avoids having a separate file column on
 * every entity; supports multiple files per entity and unlimited file types.
 */
CREATE TABLE public.attachments (
    attachment_id    BIGSERIAL    PRIMARY KEY,
    attachment_code  VARCHAR(20)  NOT NULL UNIQUE
                         DEFAULT public.next_public_code('attachments', 'ATT', 6, false),
    entity_type      VARCHAR(50)  NOT NULL,
    entity_id        BIGINT       NOT NULL,
    file_name        VARCHAR(500),
    file_url         TEXT         NOT NULL,
    file_type        VARCHAR(100),
    file_size        BIGINT,
    uploaded_by      BIGINT,
    uploaded_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    deleted_at       TIMESTAMP    -- soft delete (business rule: never hard-delete attachments)
);

/*
 * audit_logs
 * Immutable write-once log of every INSERT, UPDATE, or DELETE on key tables.
 * Stores old and new JSON snapshots. source_ip and user_agent come from the
 * HTTP request context.
 *
 * Who uses it: admin and compliance team only. Not shown to regular users.
 * Why: regulatory requirement and dispute resolution — "who changed the price
 * on this order, and when?" is answerable from audit_logs without having to
 * restore a database backup.
 */
CREATE TABLE public.audit_logs (
    audit_id    BIGSERIAL    PRIMARY KEY,
    table_name  VARCHAR(100) NOT NULL,
    record_id   BIGINT       NOT NULL,
    action_type VARCHAR(20)  NOT NULL,
    old_data    JSONB,
    new_data    JSONB,
    changed_by  BIGINT,
    source_ip   VARCHAR(100),
    user_agent  TEXT,
    changed_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);


-- =============================================================================
-- SECTION 15: DEFERRED FOREIGN KEY (circular dep: quotations ↔ orders)
-- =============================================================================

ALTER TABLE public.quotations
    ADD CONSTRAINT fk_quotations_converted_order
    FOREIGN KEY (converted_order_id) REFERENCES public.orders(order_id);


-- =============================================================================
-- SECTION 16: ALL FOREIGN KEY CONSTRAINTS
-- (grouped here so tables can be created in any order above)
-- =============================================================================

-- users (self-refs)
ALTER TABLE public.users ADD CONSTRAINT users_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(user_id);
ALTER TABLE public.users ADD CONSTRAINT users_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(user_id);

-- user_roles
ALTER TABLE public.user_roles ADD CONSTRAINT user_roles_user_id_fkey    FOREIGN KEY (user_id)     REFERENCES public.users(user_id) ON DELETE CASCADE;
ALTER TABLE public.user_roles ADD CONSTRAINT user_roles_role_id_fkey    FOREIGN KEY (role_id)     REFERENCES public.roles(role_id);
ALTER TABLE public.user_roles ADD CONSTRAINT user_roles_assigned_by_fkey FOREIGN KEY (assigned_by) REFERENCES public.users(user_id) ON DELETE SET NULL;

-- user_sessions
ALTER TABLE public.user_sessions ADD CONSTRAINT user_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(user_id) ON DELETE CASCADE;

-- user_activities
ALTER TABLE public.user_activities ADD CONSTRAINT user_activities_user_id_fkey    FOREIGN KEY (user_id)    REFERENCES public.users(user_id)         ON DELETE CASCADE;
ALTER TABLE public.user_activities ADD CONSTRAINT user_activities_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.user_sessions(session_id) ON DELETE SET NULL;

-- user_addresses
ALTER TABLE public.user_addresses ADD CONSTRAINT user_addresses_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(user_id) ON DELETE CASCADE;

-- user_subscriptions
ALTER TABLE public.user_subscriptions ADD CONSTRAINT user_subscriptions_user_id_fkey FOREIGN KEY (user_id)  REFERENCES public.users(user_id);
ALTER TABLE public.user_subscriptions ADD CONSTRAINT user_subscriptions_plan_id_fkey FOREIGN KEY (plan_id)  REFERENCES public.subscription_plans(plan_id);

-- user_notification_devices
ALTER TABLE public.user_notification_devices ADD CONSTRAINT user_notification_devices_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(user_id) ON DELETE CASCADE;

-- nurseries
ALTER TABLE public.nurseries ADD CONSTRAINT nurseries_owner_user_id_fkey  FOREIGN KEY (owner_user_id) REFERENCES public.users(user_id);
ALTER TABLE public.nurseries ADD CONSTRAINT nurseries_created_by_fkey     FOREIGN KEY (created_by)    REFERENCES public.users(user_id);
ALTER TABLE public.nurseries ADD CONSTRAINT nurseries_approved_by_fkey    FOREIGN KEY (approved_by)   REFERENCES public.users(user_id);
ALTER TABLE public.nurseries ADD CONSTRAINT nurseries_rejected_by_fkey    FOREIGN KEY (rejected_by)   REFERENCES public.users(user_id);

-- nursery_applications
ALTER TABLE public.nursery_applications ADD CONSTRAINT nursery_apps_applicant_fkey   FOREIGN KEY (applicant_user_id) REFERENCES public.users(user_id);
ALTER TABLE public.nursery_applications ADD CONSTRAINT nursery_apps_reviewed_by_fkey FOREIGN KEY (reviewed_by)       REFERENCES public.users(user_id);
ALTER TABLE public.nursery_applications ADD CONSTRAINT nursery_apps_nursery_id_fkey  FOREIGN KEY (nursery_id)        REFERENCES public.nurseries(nursery_id);

-- platform_config
ALTER TABLE public.platform_config ADD CONSTRAINT platform_config_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(user_id);

-- nursery_addresses
ALTER TABLE public.nursery_addresses ADD CONSTRAINT nursery_addresses_nursery_id_fkey FOREIGN KEY (nursery_id) REFERENCES public.nurseries(nursery_id) ON DELETE CASCADE;

-- nursery_users
ALTER TABLE public.nursery_users ADD CONSTRAINT nursery_users_nursery_id_fkey         FOREIGN KEY (nursery_id)          REFERENCES public.nurseries(nursery_id) ON DELETE CASCADE;
ALTER TABLE public.nursery_users ADD CONSTRAINT nursery_users_user_id_fkey            FOREIGN KEY (user_id)             REFERENCES public.users(user_id)        ON DELETE CASCADE;
ALTER TABLE public.nursery_users ADD CONSTRAINT nursery_users_nursery_role_id_fkey    FOREIGN KEY (nursery_role_id)     REFERENCES public.nursery_roles(nursery_role_id);
ALTER TABLE public.nursery_users ADD CONSTRAINT nursery_users_invited_by_fkey         FOREIGN KEY (invited_by_user_id)  REFERENCES public.users(user_id);

-- nursery_drivers
ALTER TABLE public.nursery_drivers ADD CONSTRAINT nursery_drivers_nursery_id_fkey          FOREIGN KEY (nursery_id)           REFERENCES public.nurseries(nursery_id) ON DELETE CASCADE;
ALTER TABLE public.nursery_drivers ADD CONSTRAINT nursery_drivers_driver_user_id_fkey      FOREIGN KEY (driver_user_id)       REFERENCES public.users(user_id);
ALTER TABLE public.nursery_drivers ADD CONSTRAINT nursery_drivers_invited_by_fkey          FOREIGN KEY (invited_by_user_id)   REFERENCES public.users(user_id);
ALTER TABLE public.nursery_drivers ADD CONSTRAINT nursery_drivers_approved_by_fkey         FOREIGN KEY (approved_by_user_id)  REFERENCES public.users(user_id);

-- nursery_inventory
ALTER TABLE public.nursery_inventory ADD CONSTRAINT nursery_inventory_nursery_id_fkey       FOREIGN KEY (nursery_id)       REFERENCES public.nurseries(nursery_id) ON DELETE CASCADE;
ALTER TABLE public.nursery_inventory ADD CONSTRAINT nursery_inventory_plant_id_fkey         FOREIGN KEY (plant_id)         REFERENCES public.plants(plant_id);
ALTER TABLE public.nursery_inventory ADD CONSTRAINT nursery_inventory_size_id_fkey          FOREIGN KEY (size_id)          REFERENCES public.plant_sizes(size_id);
ALTER TABLE public.nursery_inventory ADD CONSTRAINT nursery_inventory_last_updated_by_fkey  FOREIGN KEY (last_updated_by)  REFERENCES public.users(user_id);

-- plant_names
ALTER TABLE public.plant_names ADD CONSTRAINT plant_names_plant_id_fkey    FOREIGN KEY (plant_id)    REFERENCES public.plants(plant_id)       ON DELETE CASCADE;
ALTER TABLE public.plant_names ADD CONSTRAINT plant_names_language_id_fkey FOREIGN KEY (language_id) REFERENCES public.languages(language_id);

-- plant_category_mapping
ALTER TABLE public.plant_category_mapping ADD CONSTRAINT pcm_plant_id_fkey    FOREIGN KEY (plant_id)    REFERENCES public.plants(plant_id)           ON DELETE CASCADE;
ALTER TABLE public.plant_category_mapping ADD CONSTRAINT pcm_category_id_fkey FOREIGN KEY (category_id) REFERENCES public.plant_categories(category_id);

-- plant_images
ALTER TABLE public.plant_images ADD CONSTRAINT plant_images_plant_id_fkey FOREIGN KEY (plant_id) REFERENCES public.plants(plant_id) ON DELETE CASCADE;

-- plant_care_guides
ALTER TABLE public.plant_care_guides ADD CONSTRAINT plant_care_guides_plant_id_fkey FOREIGN KEY (plant_id) REFERENCES public.plants(plant_id) ON DELETE CASCADE;

-- plant_requests
ALTER TABLE public.plant_requests ADD CONSTRAINT plant_requests_nursery_id_fkey      FOREIGN KEY (requesting_nursery_id)  REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.plant_requests ADD CONSTRAINT plant_requests_user_id_fkey         FOREIGN KEY (requested_by_user_id)   REFERENCES public.users(user_id);
ALTER TABLE public.plant_requests ADD CONSTRAINT plant_requests_plant_id_fkey        FOREIGN KEY (plant_id)               REFERENCES public.plants(plant_id);
ALTER TABLE public.plant_requests ADD CONSTRAINT plant_requests_size_id_fkey         FOREIGN KEY (size_id)                REFERENCES public.plant_sizes(size_id);

-- plant_request_responses
ALTER TABLE public.plant_request_responses ADD CONSTRAINT prr_request_id_fkey            FOREIGN KEY (request_id)            REFERENCES public.plant_requests(request_id) ON DELETE CASCADE;
ALTER TABLE public.plant_request_responses ADD CONSTRAINT prr_supplier_nursery_id_fkey   FOREIGN KEY (supplier_nursery_id)   REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.plant_request_responses ADD CONSTRAINT prr_responded_by_user_id_fkey  FOREIGN KEY (responded_by_user_id)  REFERENCES public.users(user_id);

-- quotations
ALTER TABLE public.quotations ADD CONSTRAINT quotations_created_by_user_id_fkey        FOREIGN KEY (created_by_user_id)       REFERENCES public.users(user_id);
ALTER TABLE public.quotations ADD CONSTRAINT quotations_nursery_id_fkey                FOREIGN KEY (nursery_id)               REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.quotations ADD CONSTRAINT quotations_customer_user_id_fkey          FOREIGN KEY (customer_user_id)         REFERENCES public.users(user_id);
ALTER TABLE public.quotations ADD CONSTRAINT quotations_assigned_manager_fkey          FOREIGN KEY (assigned_manager_user_id) REFERENCES public.users(user_id);
ALTER TABLE public.quotations ADD CONSTRAINT quotations_converted_by_fkey              FOREIGN KEY (converted_by_user_id)     REFERENCES public.users(user_id);
ALTER TABLE public.quotations ADD CONSTRAINT quotations_buyer_nursery_id_fkey          FOREIGN KEY (buyer_nursery_id)         REFERENCES public.nurseries(nursery_id);

-- quotation_items
ALTER TABLE public.quotation_items ADD CONSTRAINT quotation_items_quotation_id_fkey FOREIGN KEY (quotation_id) REFERENCES public.quotations(quotation_id) ON DELETE CASCADE;
ALTER TABLE public.quotation_items ADD CONSTRAINT quotation_items_plant_id_fkey     FOREIGN KEY (plant_id)     REFERENCES public.plants(plant_id);

-- orders
ALTER TABLE public.orders ADD CONSTRAINT orders_nursery_id_fkey                  FOREIGN KEY (nursery_id)                    REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_seller_nursery_id_fkey           FOREIGN KEY (seller_nursery_id)             REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_buyer_nursery_id_fkey            FOREIGN KEY (buyer_nursery_id)              REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_quotation_id_fkey                FOREIGN KEY (quotation_id)                  REFERENCES public.quotations(quotation_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_buyer_user_id_fkey               FOREIGN KEY (buyer_user_id)                 REFERENCES public.users(user_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_customer_user_id_fkey            FOREIGN KEY (customer_user_id)              REFERENCES public.users(user_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_assigned_manager_fkey            FOREIGN KEY (assigned_manager_user_id)      REFERENCES public.users(user_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_created_by_user_id_fkey          FOREIGN KEY (created_by_user_id)            REFERENCES public.users(user_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_cancelled_by_user_id_fkey        FOREIGN KEY (cancelled_by_user_id)          REFERENCES public.users(user_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_loading_completed_by_fkey        FOREIGN KEY (loading_completed_by_user_id)  REFERENCES public.users(user_id);

-- order_items
ALTER TABLE public.order_items ADD CONSTRAINT order_items_order_id_fkey  FOREIGN KEY (order_id)  REFERENCES public.orders(order_id)      ON DELETE CASCADE;
ALTER TABLE public.order_items ADD CONSTRAINT order_items_plant_id_fkey  FOREIGN KEY (plant_id)  REFERENCES public.plants(plant_id);
ALTER TABLE public.order_items ADD CONSTRAINT order_items_size_id_fkey   FOREIGN KEY (size_id)   REFERENCES public.plant_sizes(size_id);

-- dispatches
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_order_id_fkey               FOREIGN KEY (order_id)                  REFERENCES public.orders(order_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_nursery_id_fkey             FOREIGN KEY (nursery_id)                REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_dispatched_by_fkey          FOREIGN KEY (dispatched_by)             REFERENCES public.users(user_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_manager_fkey                FOREIGN KEY (assigned_manager_user_id)  REFERENCES public.users(user_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_driver_user_id_fkey         FOREIGN KEY (driver_user_id)            REFERENCES public.users(user_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_owner_snapshot_fkey         FOREIGN KEY (owner_user_id_snapshot)    REFERENCES public.users(user_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_customer_user_id_fkey       FOREIGN KEY (customer_user_id)          REFERENCES public.users(user_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_trip_started_by_fkey        FOREIGN KEY (trip_started_by_user_id)   REFERENCES public.users(user_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_vehicle_id_fkey             FOREIGN KEY (vehicle_id)                REFERENCES public.vehicles(vehicle_id);
ALTER TABLE public.dispatches ADD CONSTRAINT dispatches_driver_id_fkey              FOREIGN KEY (driver_id)                 REFERENCES public.drivers(driver_id);

-- dispatch_items
ALTER TABLE public.dispatch_items ADD CONSTRAINT dispatch_items_dispatch_id_fkey   FOREIGN KEY (dispatch_id)   REFERENCES public.dispatches(dispatch_id) ON DELETE CASCADE;
ALTER TABLE public.dispatch_items ADD CONSTRAINT dispatch_items_order_item_id_fkey FOREIGN KEY (order_item_id) REFERENCES public.order_items(order_item_id);

-- dispatch_assignments
ALTER TABLE public.dispatch_assignments ADD CONSTRAINT da_dispatch_id_fkey  FOREIGN KEY (dispatch_id)  REFERENCES public.dispatches(dispatch_id) ON DELETE CASCADE;
ALTER TABLE public.dispatch_assignments ADD CONSTRAINT da_vehicle_id_fkey   FOREIGN KEY (vehicle_id)   REFERENCES public.vehicles(vehicle_id);
ALTER TABLE public.dispatch_assignments ADD CONSTRAINT da_driver_id_fkey    FOREIGN KEY (driver_id)    REFERENCES public.drivers(driver_id);
ALTER TABLE public.dispatch_assignments ADD CONSTRAINT da_assigned_by_fkey  FOREIGN KEY (assigned_by)  REFERENCES public.users(user_id);

-- trip_events
ALTER TABLE public.trip_events ADD CONSTRAINT trip_events_dispatch_id_fkey        FOREIGN KEY (dispatch_id)        REFERENCES public.dispatches(dispatch_id) ON DELETE CASCADE;
ALTER TABLE public.trip_events ADD CONSTRAINT trip_events_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES public.users(user_id);

-- trip_tracking_links
ALTER TABLE public.trip_tracking_links ADD CONSTRAINT ttl_dispatch_id_fkey       FOREIGN KEY (dispatch_id)      REFERENCES public.dispatches(dispatch_id) ON DELETE CASCADE;
ALTER TABLE public.trip_tracking_links ADD CONSTRAINT ttl_customer_user_id_fkey  FOREIGN KEY (customer_user_id) REFERENCES public.users(user_id);

-- vehicles
-- (no FKs beyond what's set in dispatches/dispatch_assignments)

-- drivers
ALTER TABLE public.drivers ADD CONSTRAINT drivers_user_id_fkey           FOREIGN KEY (user_id)              REFERENCES public.users(user_id);
ALTER TABLE public.drivers ADD CONSTRAINT drivers_approved_by_user_fkey  FOREIGN KEY (approved_by_user_id)  REFERENCES public.users(user_id);

-- driver_locations
ALTER TABLE public.driver_locations ADD CONSTRAINT driver_locations_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(driver_id) ON DELETE CASCADE;

-- vehicle_locations
ALTER TABLE public.vehicle_locations ADD CONSTRAINT vehicle_locations_vehicle_id_fkey FOREIGN KEY (vehicle_id) REFERENCES public.vehicles(vehicle_id) ON DELETE CASCADE;

-- vehicle_tracking
ALTER TABLE public.vehicle_tracking ADD CONSTRAINT vehicle_tracking_vehicle_id_fkey  FOREIGN KEY (vehicle_id)  REFERENCES public.vehicles(vehicle_id);
ALTER TABLE public.vehicle_tracking ADD CONSTRAINT vehicle_tracking_driver_id_fkey   FOREIGN KEY (driver_id)   REFERENCES public.drivers(driver_id);
ALTER TABLE public.vehicle_tracking ADD CONSTRAINT vehicle_tracking_dispatch_id_fkey FOREIGN KEY (dispatch_id) REFERENCES public.dispatches(dispatch_id) ON DELETE SET NULL;

-- invites
ALTER TABLE public.invites ADD CONSTRAINT invites_invited_by_user_id_fkey  FOREIGN KEY (invited_by_user_id)  REFERENCES public.users(user_id);
ALTER TABLE public.invites ADD CONSTRAINT invites_nursery_id_fkey          FOREIGN KEY (nursery_id)          REFERENCES public.nurseries(nursery_id);
ALTER TABLE public.invites ADD CONSTRAINT invites_accepted_by_user_id_fkey FOREIGN KEY (accepted_by_user_id) REFERENCES public.users(user_id);

-- payments
ALTER TABLE public.payments ADD CONSTRAINT payments_order_id_fkey              FOREIGN KEY (order_id)             REFERENCES public.orders(order_id);
ALTER TABLE public.payments ADD CONSTRAINT payments_user_subscription_id_fkey  FOREIGN KEY (user_subscription_id) REFERENCES public.user_subscriptions(user_subscription_id);
ALTER TABLE public.payments ADD CONSTRAINT payments_payer_user_id_fkey         FOREIGN KEY (payer_user_id)         REFERENCES public.users(user_id);

-- notifications
ALTER TABLE public.notifications ADD CONSTRAINT notifications_user_id_fkey     FOREIGN KEY (user_id)     REFERENCES public.users(user_id);
ALTER TABLE public.notifications ADD CONSTRAINT notifications_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.notification_templates(template_id);

-- attachments
ALTER TABLE public.attachments ADD CONSTRAINT attachments_uploaded_by_fkey FOREIGN KEY (uploaded_by) REFERENCES public.users(user_id);


-- =============================================================================
-- SECTION 17: INDEXES
-- =============================================================================

-- users
CREATE INDEX idx_users_status       ON public.users (status);
CREATE INDEX idx_users_deleted_at   ON public.users (deleted_at);

-- user_sessions
CREATE INDEX idx_user_sessions_user          ON public.user_sessions (user_id);
CREATE INDEX idx_user_sessions_last_activity ON public.user_sessions (last_activity_at);

-- user_activities
CREATE INDEX idx_user_activities_user      ON public.user_activities (user_id);
CREATE INDEX idx_user_activities_timestamp ON public.user_activities (activity_timestamp);
CREATE INDEX idx_user_activities_type      ON public.user_activities (activity_type);
CREATE INDEX idx_user_activities_entity    ON public.user_activities (entity_type, entity_id);

-- user_notification_devices
CREATE INDEX idx_user_notification_devices_user  ON public.user_notification_devices (user_id, is_active);

-- user_subscriptions
CREATE INDEX idx_user_subscriptions_user_status ON public.user_subscriptions (user_id, subscription_status);
CREATE INDEX idx_user_subscriptions_plan        ON public.user_subscriptions (plan_id);

-- nurseries
CREATE INDEX idx_nurseries_owner       ON public.nurseries (owner_user_id);
CREATE INDEX idx_nurseries_status      ON public.nurseries (status);
CREATE INDEX idx_nurseries_deleted     ON public.nurseries (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX idx_nurseries_approved_by ON public.nurseries (approved_by);

-- nursery_applications
CREATE INDEX idx_nursery_apps_applicant ON public.nursery_applications (applicant_user_id);
CREATE INDEX idx_nursery_apps_status    ON public.nursery_applications (status);
CREATE INDEX idx_nursery_apps_nursery   ON public.nursery_applications (nursery_id);

-- nursery_users
CREATE INDEX idx_nursery_users_nursery ON public.nursery_users (nursery_id);
CREATE INDEX idx_nursery_users_user    ON public.nursery_users (user_id);

-- Business rule: manager can belong to only ONE active nursery at a time
CREATE UNIQUE INDEX uq_manager_one_active_nursery
    ON public.nursery_users (user_id)
    WHERE status = 'ACTIVE';

-- nursery_drivers
CREATE INDEX idx_nursery_drivers_nursery ON public.nursery_drivers (nursery_id);
CREATE INDEX idx_nursery_drivers_driver  ON public.nursery_drivers (driver_user_id);

-- nursery_inventory
CREATE INDEX idx_inventory_nursery ON public.nursery_inventory (nursery_id);
CREATE INDEX idx_inventory_plant   ON public.nursery_inventory (plant_id);
CREATE INDEX idx_inventory_status  ON public.nursery_inventory (inventory_status);

-- plants
CREATE INDEX idx_plants_scientific_name ON public.plants (scientific_name);
CREATE INDEX idx_plants_is_active       ON public.plants (is_active);

-- plant_names
CREATE INDEX idx_plant_names_language ON public.plant_names (language_id);
CREATE INDEX idx_plant_names_name     ON public.plant_names (plant_name);

-- plant_category_mapping
CREATE INDEX idx_pcm_plant    ON public.plant_category_mapping (plant_id);
CREATE INDEX idx_pcm_category ON public.plant_category_mapping (category_id);

-- plant_images
CREATE INDEX idx_plant_images_plant ON public.plant_images (plant_id);

-- plant_requests
CREATE INDEX idx_plant_request_nursery ON public.plant_requests (requesting_nursery_id);
CREATE INDEX idx_plant_request_plant   ON public.plant_requests (plant_id);
CREATE INDEX idx_plant_request_status  ON public.plant_requests (status);

-- plant_request_responses
CREATE INDEX idx_request_response_request  ON public.plant_request_responses (request_id);
CREATE INDEX idx_request_response_supplier ON public.plant_request_responses (supplier_nursery_id);

-- quotations
CREATE INDEX idx_quotations_created_by   ON public.quotations (created_by_user_id);
CREATE INDEX idx_quotations_nursery_id   ON public.quotations (nursery_id);
CREATE INDEX idx_quotations_buyer_nursery ON public.quotations (buyer_nursery_id);

-- quotation_items
CREATE INDEX idx_quotation_items_quotation_id ON public.quotation_items (quotation_id);

-- orders
CREATE INDEX idx_orders_nursery    ON public.orders (nursery_id);
CREATE INDEX idx_orders_buyer      ON public.orders (buyer_user_id);
CREATE INDEX idx_orders_seller     ON public.orders (seller_nursery_id);
CREATE INDEX idx_orders_quotation  ON public.orders (quotation_id);
CREATE INDEX idx_orders_manager    ON public.orders (assigned_manager_user_id);
CREATE INDEX idx_orders_buyer_nursery ON public.orders (buyer_nursery_id);
CREATE INDEX idx_orders_status     ON public.orders (order_status);

-- order_items
CREATE INDEX idx_order_items_order ON public.order_items (order_id);
CREATE INDEX idx_order_items_plant ON public.order_items (plant_id);

-- dispatches
CREATE INDEX idx_dispatches_order        ON public.dispatches (order_id);
CREATE INDEX idx_dispatches_nursery      ON public.dispatches (nursery_id);
CREATE INDEX idx_dispatches_vehicle      ON public.dispatches (vehicle_id);
CREATE INDEX idx_dispatches_driver       ON public.dispatches (driver_id);
CREATE INDEX idx_dispatches_driver_user  ON public.dispatches (driver_user_id);
CREATE INDEX idx_dispatches_manager      ON public.dispatches (assigned_manager_user_id);
CREATE INDEX idx_dispatches_status       ON public.dispatches (dispatch_status);
CREATE INDEX idx_dispatches_deleted      ON public.dispatches (deleted_at) WHERE deleted_at IS NOT NULL;

-- Business rule: driver can only have ONE active trip at a time
CREATE UNIQUE INDEX uq_driver_one_active_trip
    ON public.dispatches (driver_user_id)
    WHERE dispatch_status = 'TRIP_STARTED';

-- dispatch_items
CREATE INDEX idx_dispatch_items_dispatch ON public.dispatch_items (dispatch_id);

-- dispatch_assignments
CREATE INDEX idx_dispatch_assignments_dispatch ON public.dispatch_assignments (dispatch_id);

-- trip_events
CREATE INDEX idx_trip_events_dispatch ON public.trip_events (dispatch_id);

-- trip_tracking_links
CREATE INDEX idx_tracking_links_uuid     ON public.trip_tracking_links (tracking_uuid);
CREATE INDEX idx_tracking_links_dispatch ON public.trip_tracking_links (dispatch_id);

-- drivers
CREATE INDEX idx_drivers_user_id         ON public.drivers (user_id);
CREATE INDEX idx_drivers_approval_status ON public.drivers (approval_status);

-- driver_locations
CREATE INDEX idx_driver_locations_driver_time ON public.driver_locations (driver_id, recorded_at DESC);

-- vehicle_locations
CREATE INDEX idx_vehicle_locations_vehicle  ON public.vehicle_locations (vehicle_id);
CREATE INDEX idx_vehicle_locations_recorded ON public.vehicle_locations (recorded_at);

-- vehicle_tracking
CREATE INDEX idx_vehicle_tracking_dispatch_time ON public.vehicle_tracking (dispatch_id, tracked_at DESC);

-- invites
CREATE INDEX idx_invites_uuid    ON public.invites (invite_uuid);
CREATE INDEX idx_invites_nursery ON public.invites (nursery_id);
CREATE INDEX idx_invites_status  ON public.invites (status);

-- payments
CREATE INDEX idx_payments_order        ON public.payments (order_id);
CREATE INDEX idx_payments_payer        ON public.payments (payer_user_id);
CREATE INDEX idx_payments_payment_for  ON public.payments (payment_for);
CREATE INDEX idx_payments_subscription ON public.payments (user_subscription_id);

-- notifications
CREATE INDEX idx_notifications_user_type_status_created
    ON public.notifications (user_id, notification_type, notification_status, created_at DESC);

-- notification_templates
CREATE INDEX idx_notification_templates_code_channel
    ON public.notification_templates (template_code, channel);

-- subscription_plans
CREATE INDEX idx_subscription_plans_active ON public.subscription_plans (is_active);

-- orders (soft delete)
CREATE INDEX idx_orders_deleted ON public.orders (deleted_at) WHERE deleted_at IS NOT NULL;

-- otp_requests
CREATE INDEX idx_otp_mobile_active
    ON public.otp_requests (mobile, purpose, expires_at)
    WHERE is_used = false;

-- platform_config
CREATE INDEX idx_platform_config_active ON public.platform_config (config_key) WHERE is_active = true;


-- =============================================================================
-- SECTION 18: REFERENCE SEED DATA
-- Required for the system to function. Run once on a fresh database.
-- All INSERT statements use ON CONFLICT DO NOTHING so they are safe to re-run.
-- =============================================================================

-- ─── Platform roles ───────────────────────────────────────────────────────────
INSERT INTO public.roles (role_id, role_code, role_name, description, is_active) VALUES
  (1, 'ADMIN',              'Admin',              'Platform administrator',                    true),
  (2, 'BUYER',              'Buyer',              'Plant buyer / customer',                    true),
  (3, 'NURSERY_OWNER',      'Nursery Owner',      'Nursery owner',                             true),
  (4, 'DRIVER',             'Driver',             'Delivery driver',                           true),
  (5, 'MANAGER',            'Manager',            'Nursery manager (gumastha)',                true),
  (6, 'SUPER_ADMIN',        'Super Admin',        'Platform super administrator',              true),
  (7, 'TRANSPORT_PROVIDER', 'Transport Provider', 'Fleet / transport company owner',          false),
  (8, 'CUSTOMER',           'Customer',           'Regular customer / normal user',            true)
ON CONFLICT (role_id) DO NOTHING;

SELECT setval('public.roles_role_id_seq', (SELECT MAX(role_id) FROM public.roles), true);

-- ─── Nursery roles ────────────────────────────────────────────────────────────
INSERT INTO public.nursery_roles (nursery_role_id, role_code, role_name, description, is_active) VALUES
  (1, 'OWNER',      'Owner',      'Primary owner of nursery',  true),
  (2, 'PARTNER',    'Partner',    'Business partner',          true),
  (3, 'MANAGER',    'Manager',    'Nursery manager',           true),
  (4, 'OPERATOR',   'Operator',   'Day-to-day operations',     true),
  (5, 'ACCOUNTANT', 'Accountant', 'Accounts and finance',      true),
  (6, 'DISPATCHER', 'Dispatcher', 'Dispatch operations',       true)
ON CONFLICT (role_code) DO NOTHING;

SELECT setval('public.nursery_roles_nursery_role_id_seq', (SELECT MAX(nursery_role_id) FROM public.nursery_roles), true);

-- ─── Plant sizes ──────────────────────────────────────────────────────────────
INSERT INTO public.plant_sizes (size_id, size_code, display_name, display_order, is_active) VALUES
  (1, 'SEED',        'Seed',        1, true),
  (2, 'SAPLING',     'Sapling',     2, true),
  (3, 'SMALL',       'Small',       3, true),
  (4, 'MEDIUM',      'Medium',      4, true),
  (5, 'LARGE',       'Large',       5, true),
  (6, 'EXTRA_LARGE', 'Extra Large', 6, true)
ON CONFLICT (size_id) DO NOTHING;

SELECT setval('public.plant_sizes_size_id_seq', (SELECT MAX(size_id) FROM public.plant_sizes), true);

-- ─── Plant categories ─────────────────────────────────────────────────────────
INSERT INTO public.plant_categories (category_name, is_active) VALUES
  ('Fruit Trees',      true),
  ('Medicinal Plants', true),
  ('Shade Trees',      true),
  ('Herbs',            true),
  ('Ornamental',       true),
  ('Flowering Shrubs', true),
  ('Indoor Plants',    true)
ON CONFLICT (category_name) DO NOTHING;

-- ─── Languages ────────────────────────────────────────────────────────────────
INSERT INTO public.languages (language_code, language_name, is_active) VALUES
  ('en', 'English', true),
  ('hi', 'Hindi',   true),
  ('te', 'Telugu',  true),
  ('ta', 'Tamil',   true),
  ('kn', 'Kannada', true),
  ('mr', 'Marathi', true)
ON CONFLICT (language_code) DO NOTHING;

-- ─── Public code sequence seeds (start all counters at 0) ────────────────────
INSERT INTO public.public_code_sequences (code_key, date_key, last_value) VALUES
  ('users',                 '', 0),
  ('plants',                '', 0),
  ('nurseries',             '', 0),
  ('nursery_inventory',     '', 0),
  ('nursery_applications',  '', 0),
  ('drivers',               '', 0),
  ('vehicles',              '', 0),
  ('attachments',           '', 0),
  ('notifications',         '', 0),
  ('user_subscriptions',    '', 0)
ON CONFLICT (code_key, date_key) DO NOTHING;

-- ─── Platform config defaults ─────────────────────────────────────────────────
INSERT INTO public.platform_config (config_key, config_value, data_type, description) VALUES
  ('otp_expiry_minutes',      '5',    'integer', 'OTP validity window in minutes'),
  ('otp_max_attempts',        '5',    'integer', 'Wrong OTP attempts before the code is blocked'),
  ('otp_resend_cooldown_sec', '30',   'integer', 'Seconds a user must wait before requesting another OTP'),
  ('min_order_amount',        '100',  'numeric', 'Minimum order total in INR'),
  ('platform_fee_pct',        '0',    'numeric', 'Platform fee percentage applied to orders'),
  ('driver_approval_days',    '3',    'integer', 'Days before a pending driver approval auto-expires'),
  ('nursery_approval_days',   '7',    'integer', 'Days before a pending nursery application auto-expires')
ON CONFLICT (config_key) DO NOTHING;

-- ─── Dev admin user (login: 9000000777, any OTP works in DEV mode) ─────────────
INSERT INTO public.users (user_id, user_code, first_name, mobile, mobile_verified, status)
VALUES (1, 'USR-000001', 'Admin', '9000000777', true, 'ACTIVE')
ON CONFLICT (mobile) DO NOTHING;

INSERT INTO public.user_roles (user_id, role_id)
SELECT 1, role_id FROM public.roles WHERE role_code IN ('ADMIN', 'SUPER_ADMIN')
ON CONFLICT DO NOTHING;

SELECT setval('public.users_user_id_seq', (SELECT MAX(user_id) FROM public.users), true);

-- Sync public_code_sequences so the next auto-generated user_code won't conflict
INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
SELECT 'users', '', count(*) FROM public.users
ON CONFLICT (code_key, date_key) DO UPDATE
    SET last_value = GREATEST(public_code_sequences.last_value, EXCLUDED.last_value),
        updated_at = CURRENT_TIMESTAMP;
