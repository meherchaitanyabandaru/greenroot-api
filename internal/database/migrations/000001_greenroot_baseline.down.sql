-- GreenRoot baseline rollback.
-- WARNING: this drops the public schema and all GreenRoot data in the target database.
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;
GRANT ALL ON SCHEMA public TO PUBLIC;
