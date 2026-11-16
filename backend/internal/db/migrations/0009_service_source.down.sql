DROP INDEX IF EXISTS idx_services_source;
ALTER TABLE services DROP COLUMN IF EXISTS source;
