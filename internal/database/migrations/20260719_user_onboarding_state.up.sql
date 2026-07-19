ALTER TABLE public.users
  ADD COLUMN IF NOT EXISTS onboarding_completed boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS initial_activity character varying(30),
  ADD COLUMN IF NOT EXISTS onboarding_completed_at timestamp without time zone;

ALTER TABLE public.users
  ADD CONSTRAINT chk_users_initial_activity
  CHECK (
    initial_activity IS NULL
    OR initial_activity IN ('CUSTOMER', 'OWNER', 'DRIVER', 'MANAGER')
  );

UPDATE public.users
SET onboarding_completed = true,
    initial_activity = COALESCE(initial_activity, 'CUSTOMER'),
    onboarding_completed_at = COALESCE(onboarding_completed_at, updated_at, created_at, CURRENT_TIMESTAMP)
WHERE onboarding_completed = false
  AND deleted_at IS NULL
  AND first_name IS NOT NULL
  AND BTRIM(first_name) <> ''
  AND LOWER(BTRIM(first_name)) <> 'greenroot';
