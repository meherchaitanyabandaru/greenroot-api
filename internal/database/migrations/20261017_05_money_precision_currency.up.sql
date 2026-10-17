-- =============================================================================
-- Migration 2026-10-17 005: one money type (standard D2)
-- =============================================================================
-- Amounts were numeric(12,2) on quotations but numeric(15,2) on the orders a
-- quotation converts into, so a large quotation could not always become an
-- order. Every money column is now numeric(15,2). Widening precision at the
-- same scale needs no table rewrite and changes no stored value.
--
-- Each money-bearing aggregate also states its currency. Only INR is traded
-- today, so the CHECK pins it; supporting another currency is a deliberate
-- migration (relax the CHECK, add conversion rules), not an accident.
-- =============================================================================

ALTER TABLE public.quotations
    ALTER COLUMN total_amount TYPE numeric(15,2);
ALTER TABLE public.quotation_items
    ALTER COLUMN unit_price  TYPE numeric(15,2),
    ALTER COLUMN total_price TYPE numeric(15,2);
ALTER TABLE public.subscription_plans
    ALTER COLUMN monthly_price TYPE numeric(15,2),
    ALTER COLUMN yearly_price  TYPE numeric(15,2);
ALTER TABLE public.subscription_promos
    ALTER COLUMN discount_value   TYPE numeric(15,2),
    ALTER COLUMN max_discount_cap TYPE numeric(15,2);
ALTER TABLE public.market_ads
    ALTER COLUMN price_per_unit TYPE numeric(15,2);

ALTER TABLE public.orders             ADD COLUMN IF NOT EXISTS currency character(3) NOT NULL DEFAULT 'INR';
ALTER TABLE public.quotations         ADD COLUMN IF NOT EXISTS currency character(3) NOT NULL DEFAULT 'INR';
ALTER TABLE public.payments           ADD COLUMN IF NOT EXISTS currency character(3) NOT NULL DEFAULT 'INR';
ALTER TABLE public.subscription_plans ADD COLUMN IF NOT EXISTS currency character(3) NOT NULL DEFAULT 'INR';

ALTER TABLE public.orders             ADD CONSTRAINT ck_orders_currency             CHECK (currency = 'INR');
ALTER TABLE public.quotations         ADD CONSTRAINT ck_quotations_currency         CHECK (currency = 'INR');
ALTER TABLE public.payments           ADD CONSTRAINT ck_payments_currency           CHECK (currency = 'INR');
ALTER TABLE public.subscription_plans ADD CONSTRAINT ck_subscription_plans_currency CHECK (currency = 'INR');
