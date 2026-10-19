-- =============================================================================
-- Migration 2026-10-19 004: one readable number per order and dispatch
-- =============================================================================
-- Orders created directly got order_number GR-ORD-<nanoseconds>, orders
-- converted from a quotation got ORD-YYYYMMDD-NNNN-<digits>, and dispatches
-- got GR-DSP-<nanoseconds>. The API now uses the public code (order_code,
-- dispatch_code) when no number is given; this aligns existing rows.
-- Numbers a user typed in themselves are left alone.
-- =============================================================================

UPDATE public.orders
SET order_number = order_code
WHERE order_number ~ '^GR-ORD-[0-9]+$'
   OR order_number = order_code || '-' || substring(order_number FROM '[0-9]+$');

UPDATE public.dispatches
SET dispatch_number = dispatch_code
WHERE dispatch_number IS NULL OR dispatch_number ~ '^GR-DSP-[0-9]+$';
