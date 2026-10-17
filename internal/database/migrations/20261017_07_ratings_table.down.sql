-- Rollback for 20261017_07_ratings_table (manual; the runner only applies up). Deletes all ratings.
DROP TABLE IF EXISTS public.ratings;
