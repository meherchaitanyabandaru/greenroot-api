-- ============================================================
-- 000006 — Local Market
-- Private B2B marketplace for Nursery Owners and Managers.
-- Drivers and Buyers have no access.
-- ============================================================

-- ── market_listings ──────────────────────────────────────────
-- Core listing entity. Photos stored as ordered JSONB array;
-- first element is the cover photo (max 10 total).
-- Status machine: DRAFT → PUBLISHED → PAUSED → EXPIRED → ARCHIVED
CREATE TABLE public.market_listings (
    listing_id          BIGSERIAL    PRIMARY KEY,
    listing_code        VARCHAR(30)  NOT NULL UNIQUE
                            DEFAULT public.next_public_code('market_listings', 'MKT', 6, false),
    nursery_id          BIGINT       NOT NULL,
    created_by_user_id  BIGINT       NOT NULL,
    updated_by_user_id  BIGINT,

    -- Plant identity (stored in full so listing survives catalogue changes)
    plant_id            BIGINT,
    plant_name          VARCHAR(255) NOT NULL,
    category_name       VARCHAR(100),

    -- Listing content
    title               VARCHAR(255) NOT NULL,
    description         TEXT,
    quantity            INTEGER,
    size_description    VARCHAR(100),
    price_per_unit      NUMERIC(12,2),
    price_unit          VARCHAR(50),  -- 'per plant', 'per dozen', etc.

    -- Photos: index 0 = cover, rest = additional (max 10 total)
    photos              JSONB        NOT NULL DEFAULT '[]',

    -- Status machine
    status              VARCHAR(20)  NOT NULL DEFAULT 'DRAFT',

    -- Counters (denormalised for cheap reads)
    view_count          INTEGER      NOT NULL DEFAULT 0,
    save_count          INTEGER      NOT NULL DEFAULT 0,
    enquiry_count       INTEGER      NOT NULL DEFAULT 0,

    -- Lifecycle timestamps
    published_at        TIMESTAMP,
    paused_at           TIMESTAMP,
    expired_at          TIMESTAMP,
    archived_at         TIMESTAMP,
    expires_at          TIMESTAMP,   -- auto-expire after 30 days from publish

    created_at          TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT chk_market_listing_status CHECK (
        status IN ('DRAFT', 'PUBLISHED', 'PAUSED', 'EXPIRED', 'ARCHIVED')
    )
);

CREATE INDEX idx_market_listings_nursery    ON public.market_listings (nursery_id);
CREATE INDEX idx_market_listings_status     ON public.market_listings (status);
CREATE INDEX idx_market_listings_expires_at ON public.market_listings (expires_at) WHERE status = 'PUBLISHED';
CREATE INDEX idx_market_listings_plant      ON public.market_listings (plant_name);

-- ── market_listing_saves ─────────────────────────────────────
-- One bookmark per (listing, saving nursery). Unique constraint prevents duplicates.
CREATE TABLE public.market_listing_saves (
    save_id             BIGSERIAL    PRIMARY KEY,
    listing_id          BIGINT       NOT NULL,
    nursery_id          BIGINT       NOT NULL,
    saved_by_user_id    BIGINT       NOT NULL,
    saved_at            TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_market_listing_save UNIQUE (listing_id, nursery_id)
);

CREATE INDEX idx_market_saves_nursery ON public.market_listing_saves (nursery_id);

-- ── market_listing_views ─────────────────────────────────────
-- One view per (listing, viewing nursery) — deduplicated per nursery.
CREATE TABLE public.market_listing_views (
    view_id             BIGSERIAL    PRIMARY KEY,
    listing_id          BIGINT       NOT NULL,
    nursery_id          BIGINT,
    viewed_at           TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_market_listing_view UNIQUE (listing_id, nursery_id)
);

-- ── market_listing_reports ───────────────────────────────────
-- Admin moderation reports. One report per (listing, reporting user).
CREATE TABLE public.market_listing_reports (
    report_id               BIGSERIAL    PRIMARY KEY,
    listing_id              BIGINT       NOT NULL,
    reported_by_user_id     BIGINT       NOT NULL,
    reported_by_nursery_id  BIGINT       NOT NULL,
    reason                  VARCHAR(50)  NOT NULL,
    notes                   TEXT,
    status                  VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    reviewed_by_user_id     BIGINT,
    reviewed_at             TIMESTAMP,
    created_at              TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT chk_market_report_reason CHECK (
        reason IN ('SPAM', 'WRONG_PLANT', 'DUPLICATE', 'FRAUD', 'OTHER')
    ),
    CONSTRAINT chk_market_report_status CHECK (
        status IN ('PENDING', 'REVIEWED', 'DISMISSED')
    ),
    CONSTRAINT uq_market_listing_report UNIQUE (listing_id, reported_by_user_id)
);

-- ── market_enquiries ─────────────────────────────────────────
-- Enquiry from one nursery to the listing nursery.
-- Status machine: NEW → IN_PROGRESS → QUOTATION_CREATED → CLOSED | CANCELLED
CREATE TABLE public.market_enquiries (
    enquiry_id              BIGSERIAL    PRIMARY KEY,
    enquiry_code            VARCHAR(30)  NOT NULL UNIQUE
                                DEFAULT public.next_public_code('market_enquiries', 'ENQ', 6, false),
    listing_id              BIGINT       NOT NULL,
    listing_nursery_id      BIGINT       NOT NULL,
    enquiring_nursery_id    BIGINT       NOT NULL,
    created_by_user_id      BIGINT       NOT NULL,

    -- Initial enquiry message
    message                 TEXT         NOT NULL,
    quantity_needed         INTEGER,

    -- Status machine
    status                  VARCHAR(25)  NOT NULL DEFAULT 'NEW',

    -- Optional link once a quotation is created in the main workflow
    quotation_id            BIGINT,

    -- Lifecycle timestamps
    viewed_at               TIMESTAMP,
    replied_at              TIMESTAMP,
    closed_at               TIMESTAMP,
    cancelled_at            TIMESTAMP,
    created_at              TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- One enquiry per nursery per listing
    CONSTRAINT uq_market_enquiry UNIQUE (listing_id, enquiring_nursery_id),
    CONSTRAINT chk_market_enquiry_status CHECK (
        status IN ('NEW', 'IN_PROGRESS', 'QUOTATION_CREATED', 'CLOSED', 'CANCELLED')
    )
);

CREATE INDEX idx_market_enquiries_listing  ON public.market_enquiries (listing_id);
CREATE INDEX idx_market_enquiries_listing_nursery ON public.market_enquiries (listing_nursery_id);
CREATE INDEX idx_market_enquiries_enquiring ON public.market_enquiries (enquiring_nursery_id);

-- ── market_enquiry_messages ──────────────────────────────────
-- Thread of messages within an enquiry (initial + replies).
CREATE TABLE public.market_enquiry_messages (
    message_id          BIGSERIAL    PRIMARY KEY,
    enquiry_id          BIGINT       NOT NULL,
    sent_by_user_id     BIGINT       NOT NULL,
    sent_by_nursery_id  BIGINT       NOT NULL,
    body                TEXT         NOT NULL,
    created_at          TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_market_enquiry_messages_enquiry ON public.market_enquiry_messages (enquiry_id);

-- ── Foreign keys ─────────────────────────────────────────────
ALTER TABLE public.market_listings
    ADD CONSTRAINT market_listings_nursery_fkey       FOREIGN KEY (nursery_id)         REFERENCES public.nurseries(nursery_id),
    ADD CONSTRAINT market_listings_created_by_fkey    FOREIGN KEY (created_by_user_id) REFERENCES public.users(user_id),
    ADD CONSTRAINT market_listings_updated_by_fkey    FOREIGN KEY (updated_by_user_id) REFERENCES public.users(user_id),
    ADD CONSTRAINT market_listings_plant_fkey         FOREIGN KEY (plant_id)           REFERENCES public.plants(plant_id);

ALTER TABLE public.market_listing_saves
    ADD CONSTRAINT market_saves_listing_fkey   FOREIGN KEY (listing_id)       REFERENCES public.market_listings(listing_id),
    ADD CONSTRAINT market_saves_nursery_fkey   FOREIGN KEY (nursery_id)       REFERENCES public.nurseries(nursery_id),
    ADD CONSTRAINT market_saves_user_fkey      FOREIGN KEY (saved_by_user_id) REFERENCES public.users(user_id);

ALTER TABLE public.market_listing_views
    ADD CONSTRAINT market_views_listing_fkey  FOREIGN KEY (listing_id) REFERENCES public.market_listings(listing_id),
    ADD CONSTRAINT market_views_nursery_fkey  FOREIGN KEY (nursery_id) REFERENCES public.nurseries(nursery_id);

ALTER TABLE public.market_listing_reports
    ADD CONSTRAINT market_reports_listing_fkey FOREIGN KEY (listing_id)             REFERENCES public.market_listings(listing_id),
    ADD CONSTRAINT market_reports_user_fkey    FOREIGN KEY (reported_by_user_id)    REFERENCES public.users(user_id),
    ADD CONSTRAINT market_reports_nursery_fkey FOREIGN KEY (reported_by_nursery_id) REFERENCES public.nurseries(nursery_id);

ALTER TABLE public.market_enquiries
    ADD CONSTRAINT market_enquiries_listing_fkey   FOREIGN KEY (listing_id)           REFERENCES public.market_listings(listing_id),
    ADD CONSTRAINT market_enquiries_listing_nursery_fkey FOREIGN KEY (listing_nursery_id)  REFERENCES public.nurseries(nursery_id),
    ADD CONSTRAINT market_enquiries_enquiring_fkey FOREIGN KEY (enquiring_nursery_id) REFERENCES public.nurseries(nursery_id),
    ADD CONSTRAINT market_enquiries_user_fkey      FOREIGN KEY (created_by_user_id)   REFERENCES public.users(user_id),
    ADD CONSTRAINT market_enquiries_quotation_fkey FOREIGN KEY (quotation_id)         REFERENCES public.quotations(quotation_id);

ALTER TABLE public.market_enquiry_messages
    ADD CONSTRAINT market_msg_enquiry_fkey FOREIGN KEY (enquiry_id)        REFERENCES public.market_enquiries(enquiry_id),
    ADD CONSTRAINT market_msg_user_fkey    FOREIGN KEY (sent_by_user_id)   REFERENCES public.users(user_id),
    ADD CONSTRAINT market_msg_nursery_fkey FOREIGN KEY (sent_by_nursery_id) REFERENCES public.nurseries(nursery_id);
