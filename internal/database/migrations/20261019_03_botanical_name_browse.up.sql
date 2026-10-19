-- =============================================================================
-- Migration 2026-10-19 003: browse all botanical names in the admin UI
-- =============================================================================
-- Admin → Plants pages through all ~1.66 M names sorted by name, and
-- Admin → Categories opens one family at a time. Without these indexes a deep
-- page (OFFSET 100000) sorts the whole table: ~2.9 s instead of milliseconds.
-- wfo_id is the tie-breaker so paging is stable for equal names.
-- =============================================================================

CREATE INDEX IF NOT EXISTS idx_botanical_names_name_order ON public.botanical_names (scientific_name, wfo_id);
CREATE INDEX IF NOT EXISTS idx_botanical_names_family ON public.botanical_names (family, scientific_name, wfo_id);
