ALTER TABLE service_endpoints
    DROP COLUMN IF EXISTS health_enabled,
    DROP COLUMN IF EXISTS last_checked_at,
    DROP COLUMN IF EXISTS last_status,
    DROP COLUMN IF EXISTS last_latency_ms,
    DROP COLUMN IF EXISTS last_error;
