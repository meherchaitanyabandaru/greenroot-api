ALTER TABLE public.users
  DROP CONSTRAINT IF EXISTS chk_users_initial_activity;

ALTER TABLE public.users
  DROP COLUMN IF EXISTS onboarding_completed_at,
  DROP COLUMN IF EXISTS initial_activity,
  DROP COLUMN IF EXISTS onboarding_completed;
