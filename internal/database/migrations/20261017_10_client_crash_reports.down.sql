-- Rollback for 20261017_10_client_crash_reports (manual; the runner only applies up). Deletes all reports.
DROP TABLE IF EXISTS public.client_crash_reports;
