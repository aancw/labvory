-- Service endpoint health checks: the scheduler probes enabled HTTP(S)
-- endpoints and records the observed result; declared endpoint data is
-- untouched.

ALTER TABLE service_endpoints
    ADD COLUMN health_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN last_checked_at TIMESTAMPTZ,
    ADD COLUMN last_status SMALLINT,
    ADD COLUMN last_latency_ms INTEGER,
    ADD COLUMN last_error TEXT;
