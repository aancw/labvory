-- Rolling health-check history for uptime reporting. The scheduler appends
-- one row per probe and prunes entries older than the retention window, so
-- the table stays bounded on homelab scale.

CREATE TABLE service_endpoint_checks (
    id          BIGSERIAL PRIMARY KEY,
    endpoint_id UUID NOT NULL REFERENCES service_endpoints (id) ON DELETE CASCADE,
    checked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    up          BOOLEAN NOT NULL,
    status      SMALLINT,
    latency_ms  INTEGER,
    error       TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_endpoint_checks_ep_time ON service_endpoint_checks (endpoint_id, checked_at);
CREATE INDEX idx_endpoint_checks_time ON service_endpoint_checks (checked_at);
