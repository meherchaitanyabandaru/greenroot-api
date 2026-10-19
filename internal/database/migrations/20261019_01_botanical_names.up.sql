-- =============================================================================
-- Migration 2026-10-19 001: botanical names reference (World Flora Online)
-- =============================================================================
-- botanical_names holds every scientific plant name from the WFO taxonomic
-- backbone (~1.66 M rows, accepted names and synonyms). It is reference data,
-- not the catalog: public.plants keeps only what nurseries sell and links to a
-- name through plants.wfo_id. Rows are loaded by
-- `go run ./cmd/botanicalnames load`, not by this migration.
--
-- import_checkpoints records how many rows of a bulk file are loaded, so an
-- interrupted load resumes exactly where it stopped.
--
-- plants.wfo_id has no foreign key on purpose: the catalog seed is loaded on
-- databases where the 1.66 M names are not (yet) imported.
-- =============================================================================

CREATE TABLE IF NOT EXISTS public.botanical_names (
    botanical_name_id bigserial PRIMARY KEY,
    wfo_id            text NOT NULL UNIQUE,
    scientific_name   text NOT NULL,
    authorship        text,
    taxon_rank        text NOT NULL,
    family            text,
    genus             text,
    status            text NOT NULL,
    accepted_wfo_id   text,
    CONSTRAINT ck_botanical_names_status CHECK (status IN ('ACCEPTED', 'SYNONYM', 'UNCHECKED')),
    CONSTRAINT ck_botanical_names_accepted CHECK (status = 'SYNONYM' OR accepted_wfo_id IS NULL)
);

CREATE INDEX IF NOT EXISTS idx_botanical_names_scientific_name ON public.botanical_names (lower(scientific_name));
CREATE INDEX IF NOT EXISTS idx_botanical_names_accepted_wfo_id ON public.botanical_names (accepted_wfo_id) WHERE accepted_wfo_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS public.import_checkpoints (
    import_checkpoint_id bigserial PRIMARY KEY,
    source_name          text NOT NULL UNIQUE,
    rows_done            bigint NOT NULL DEFAULT 0,
    updated_at           timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT ck_import_checkpoints_rows_done CHECK (rows_done >= 0)
);

ALTER TABLE public.plants ADD COLUMN IF NOT EXISTS wfo_id text;
CREATE UNIQUE INDEX IF NOT EXISTS uq_plants_wfo_id ON public.plants (wfo_id) WHERE wfo_id IS NOT NULL;
