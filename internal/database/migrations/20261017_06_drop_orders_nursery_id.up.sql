-- =============================================================================
-- Migration 2026-10-17 006: drop the dead orders.nursery_id column
-- =============================================================================
-- NULL on every row; the seller is orders.seller_nursery_id. The API stopped
-- reading it in the previous change (expand/contract: stop using, then drop).
-- No production deployment has ever run a binary that reads it.
-- =============================================================================

ALTER TABLE public.orders DROP COLUMN IF EXISTS nursery_id;
