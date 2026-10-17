-- =============================================================================
-- Migration 2026-10-17 003: row versions for optimistic concurrency (D5)
-- =============================================================================
-- Every UPDATE of these aggregates bumps `version`, enforced by a trigger so
-- no code path can forget. Writers that must not lose a concurrent change
-- update with `... WHERE id = $1 AND version = $2` and treat 0 rows as 409.
-- Adding a NOT NULL column with a constant default is metadata-only in
-- PostgreSQL 11+, so this does not rewrite the tables.
-- =============================================================================

CREATE OR REPLACE FUNCTION public.bump_row_version() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION public.bump_row_version() IS
    'BEFORE UPDATE trigger: increments version on every update (optimistic locking).';

ALTER TABLE public.orders            ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
ALTER TABLE public.quotations        ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
ALTER TABLE public.payments          ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
ALTER TABLE public.dispatches        ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
ALTER TABLE public.nursery_inventory ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

CREATE OR REPLACE TRIGGER trg_orders_version            BEFORE UPDATE ON public.orders            FOR EACH ROW EXECUTE FUNCTION public.bump_row_version();
CREATE OR REPLACE TRIGGER trg_quotations_version        BEFORE UPDATE ON public.quotations        FOR EACH ROW EXECUTE FUNCTION public.bump_row_version();
CREATE OR REPLACE TRIGGER trg_payments_version          BEFORE UPDATE ON public.payments          FOR EACH ROW EXECUTE FUNCTION public.bump_row_version();
CREATE OR REPLACE TRIGGER trg_dispatches_version        BEFORE UPDATE ON public.dispatches        FOR EACH ROW EXECUTE FUNCTION public.bump_row_version();
CREATE OR REPLACE TRIGGER trg_nursery_inventory_version BEFORE UPDATE ON public.nursery_inventory FOR EACH ROW EXECUTE FUNCTION public.bump_row_version();
