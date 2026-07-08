-- ─────────────────────────────────────────────────────────────────────────────
-- 000008_quotations_hardening.up.sql
-- Tightens the quotations schema:
--   1. Adds quotation_type as a real stored column (stop deriving from status prefix)
--   2. Adds CHECK constraints on status + quotation_type
--   3. Drops dead columns (customer_name, customer_mobile on quotations;
--      plant_name_snapshot, size, remarks on quotation_items)
--   4. Adds FK constraints
--   5. Replaces full-table indexes with partial indexes (WHERE deleted_at IS NULL)
--   6. Adds valid_until for expiry support
-- ─────────────────────────────────────────────────────────────────────────────

BEGIN;

-- ── 1. Add quotation_type column ─────────────────────────────────────────────
ALTER TABLE public.quotations
    ADD COLUMN IF NOT EXISTS quotation_type VARCHAR(20) NOT NULL DEFAULT 'CUSTOMER';

-- Back-fill from status prefix so existing rows are correct
UPDATE public.quotations
    SET quotation_type = 'INTERNAL'
    WHERE status LIKE 'INTERNAL%'
      AND quotation_type = 'CUSTOMER';

ALTER TABLE public.quotations
    ADD CONSTRAINT chk_quotation_type
        CHECK (quotation_type IN ('INTERNAL', 'CUSTOMER'));

-- ── 2. Status CHECK constraint ────────────────────────────────────────────────
-- First fix any lingering legacy values from old seed data
UPDATE public.quotations SET status = 'CUSTOMER_DRAFT'    WHERE status = 'DRAFT';
UPDATE public.quotations SET status = 'CUSTOMER_SENT'     WHERE status IN ('APPROVED', 'SENT');
UPDATE public.quotations SET status = 'CUSTOMER_ACCEPTED' WHERE status IN ('ACCEPTED', 'BUYER_ACCEPTED');

ALTER TABLE public.quotations
    ADD CONSTRAINT chk_quotation_status
        CHECK (status IN (
            'INTERNAL_DRAFT',
            'CUSTOMER_DRAFT',
            'CUSTOMER_SENT',
            'CUSTOMER_ACCEPTED',
            'CUSTOMER_REJECTED',
            'CONVERTED',
            'DELETED'
        ));

-- Fix the default from 'DRAFT' (legacy) to 'CUSTOMER_DRAFT'
ALTER TABLE public.quotations
    ALTER COLUMN status SET DEFAULT 'CUSTOMER_DRAFT';

-- ── 3. Drop dead columns ──────────────────────────────────────────────────────
ALTER TABLE public.quotations
    DROP COLUMN IF EXISTS customer_name,
    DROP COLUMN IF EXISTS customer_mobile;

ALTER TABLE public.quotation_items
    DROP COLUMN IF EXISTS plant_name_snapshot,
    DROP COLUMN IF EXISTS size,
    DROP COLUMN IF EXISTS remarks;

-- ── 4. FK constraints ─────────────────────────────────────────────────────────
-- Use IF NOT EXISTS guard via DO block to avoid re-run errors
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_quot_nursery'
    ) THEN
        ALTER TABLE public.quotations
            ADD CONSTRAINT fk_quot_nursery
                FOREIGN KEY (nursery_id) REFERENCES public.nurseries(nursery_id);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_quot_customer'
    ) THEN
        ALTER TABLE public.quotations
            ADD CONSTRAINT fk_quot_customer
                FOREIGN KEY (customer_user_id) REFERENCES public.users(user_id);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_quot_manager'
    ) THEN
        ALTER TABLE public.quotations
            ADD CONSTRAINT fk_quot_manager
                FOREIGN KEY (assigned_manager_user_id) REFERENCES public.users(user_id);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_quot_order'
    ) THEN
        ALTER TABLE public.quotations
            ADD CONSTRAINT fk_quot_order
                FOREIGN KEY (converted_order_id) REFERENCES public.orders(order_id);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_quot_items_quotation'
    ) THEN
        ALTER TABLE public.quotation_items
            ADD CONSTRAINT fk_quot_items_quotation
                FOREIGN KEY (quotation_id) REFERENCES public.quotations(quotation_id)
                ON DELETE CASCADE;
    END IF;
END $$;

-- ── 5. Replace full-table indexes with partial indexes ────────────────────────
DROP INDEX IF EXISTS public.idx_quotations_created_by;
DROP INDEX IF EXISTS public.idx_quotations_nursery_id;
DROP INDEX IF EXISTS public.idx_quotations_buyer_nursery;

CREATE INDEX IF NOT EXISTS idx_quotations_created_by
    ON public.quotations (created_by_user_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_quotations_nursery
    ON public.quotations (nursery_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_quotations_buyer_nursery
    ON public.quotations (buyer_nursery_id)
    WHERE deleted_at IS NULL;

-- New: partial index for mobile-match buyer lookups (canView + scopeList)
CREATE INDEX IF NOT EXISTS idx_quotations_recipient_mobile
    ON public.quotations (recipient_mobile)
    WHERE deleted_at IS NULL AND recipient_mobile IS NOT NULL;

-- ── 6. Add valid_until for quotation expiry ───────────────────────────────────
ALTER TABLE public.quotations
    ADD COLUMN IF NOT EXISTS valid_until TIMESTAMP;

-- Back-fill: any currently CUSTOMER_SENT quotation gets 15-day expiry from creation
UPDATE public.quotations
    SET valid_until = created_at + INTERVAL '15 days'
    WHERE status = 'CUSTOMER_SENT'
      AND valid_until IS NULL
      AND deleted_at IS NULL;

COMMIT;
