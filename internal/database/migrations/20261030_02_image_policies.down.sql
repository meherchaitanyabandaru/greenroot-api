-- Manual rollback: the API then uses its built-in default policies.
DROP TABLE IF EXISTS public.image_policy_versions;
DROP TABLE IF EXISTS public.image_policies;
