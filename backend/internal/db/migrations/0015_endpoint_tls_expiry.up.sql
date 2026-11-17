-- TLS certificate expiry observation: the scheduler captures the leaf
-- certificate served during HTTPS health-check probes so expiring certs can
-- be surfaced on the dashboard. Declared endpoint data is untouched.

ALTER TABLE service_endpoints
    ADD COLUMN cert_not_after TIMESTAMPTZ,
    ADD COLUMN cert_subject TEXT NOT NULL DEFAULT '';
