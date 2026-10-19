DROP INDEX IF EXISTS public.uq_plants_wfo_id;
ALTER TABLE public.plants DROP COLUMN IF EXISTS wfo_id;
DROP TABLE IF EXISTS public.import_checkpoints;
DROP TABLE IF EXISTS public.botanical_names;
