-- Manual rollback. Consent records are evidence: export them before dropping.
DROP TABLE IF EXISTS public.risk_signal_notes;
DROP TABLE IF EXISTS public.risk_signals;
DROP TABLE IF EXISTS public.risk_rules;
ALTER TABLE public.dispatches DROP COLUMN IF EXISTS requires_consent;
DROP TABLE IF EXISTS public.consent_records;
DROP FUNCTION IF EXISTS public.forbid_change();
