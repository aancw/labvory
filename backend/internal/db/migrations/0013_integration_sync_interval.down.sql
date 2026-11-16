ALTER TABLE integrations
    DROP CONSTRAINT IF EXISTS integrations_sync_interval_check,
    DROP COLUMN IF EXISTS sync_interval_seconds;
