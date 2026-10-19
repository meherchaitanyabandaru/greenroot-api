-- =============================================================================
-- Migration 2026-10-19 002: fast substring search over botanical names
-- =============================================================================
-- Plant pickers search 1.66 M names as the user types ("cumini" must find
-- "Syzygium cumini"), so a btree prefix index is not enough. pg_trgm makes
-- `LIKE '%term%'` use a GIN index.
--
-- botanical_search_key() is the one normalisation used by both the index and
-- the query: lower case, hybrid sign removed, so "malus domestica" finds
-- "Malus × domestica".
--
-- pg_trgm is a trusted extension; if the migrator role may not create it,
-- run `CREATE EXTENSION pg_trgm;` once as the database admin, then migrate.
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE OR REPLACE FUNCTION public.botanical_search_key(name text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT lower(replace(replace(name, '× ', ''), '×', '')) $$;

DROP INDEX IF EXISTS public.idx_botanical_names_scientific_name;
CREATE INDEX IF NOT EXISTS idx_botanical_names_search
    ON public.botanical_names USING gin (public.botanical_search_key(scientific_name) public.gin_trgm_ops);
