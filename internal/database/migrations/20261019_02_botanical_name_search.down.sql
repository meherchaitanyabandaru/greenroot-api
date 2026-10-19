DROP INDEX IF EXISTS public.idx_botanical_names_search;
DROP FUNCTION IF EXISTS public.botanical_search_key(text);
CREATE INDEX IF NOT EXISTS idx_botanical_names_scientific_name ON public.botanical_names (lower(scientific_name));
