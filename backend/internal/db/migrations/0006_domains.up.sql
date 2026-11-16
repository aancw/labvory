-- DNS inventory: domains (zones) and their subdomains/records.
-- Declared values are operator-owned; observed_* is filled by DNS resolution
-- and integration syncs (same declared-vs-observed split as other domains).

CREATE TABLE domains (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,             -- apex/zone, e.g. example.com
    registrar   TEXT NOT NULL DEFAULT '',
    expires_on  DATE,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subdomains (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id      UUID NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
    label          TEXT NOT NULL DEFAULT '',      -- '' = apex record (name = domain)
    record_type    TEXT NOT NULL DEFAULT 'A'
                   CHECK (record_type IN ('A', 'AAAA', 'CNAME', 'MX', 'TXT', 'SRV', 'CAA', 'NS')),
    value          TEXT NOT NULL DEFAULT '',      -- declared target (operator-owned)
    ttl            INTEGER CHECK (ttl IS NULL OR ttl BETWEEN 0 AND 2147483647),
    ip_address_id  UUID REFERENCES ip_addresses (id) ON DELETE SET NULL,
    service_id     UUID REFERENCES services (id) ON DELETE SET NULL,
    observed_value TEXT NOT NULL DEFAULT '',      -- last DNS answer (observed)
    observed_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (domain_id, label, record_type)
);

CREATE INDEX idx_subdomains_domain ON subdomains (domain_id);
CREATE INDEX idx_subdomains_ip ON subdomains (ip_address_id);
