-- Per-connector auto-sync schedule. NULL = manual sync only ("Sync now");
-- otherwise the scheduler runs enabled connectors when last_sync_at falls
-- more than this many seconds behind (300s … 7d).

ALTER TABLE integrations
    ADD COLUMN sync_interval_seconds INTEGER,
    ADD CONSTRAINT integrations_sync_interval_check
        CHECK (sync_interval_seconds IS NULL OR
               (sync_interval_seconds >= 300 AND sync_interval_seconds <= 604800));
