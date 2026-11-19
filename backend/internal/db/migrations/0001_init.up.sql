-- LabVory initial schema: full normalized model (see ARCHITECTURE.md ERD).

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'admin'
                  CHECK (role IN ('admin', 'operator', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    source_ip  INET,
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);

CREATE TABLE locations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    parent_id   UUID REFERENCES locations (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_locations_parent ON locations (parent_id);

-- Reference table for device categories; seeded with system defaults in 0002_defaults.
CREATE TABLE asset_types (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug      TEXT NOT NULL UNIQUE,
    label     TEXT NOT NULL,
    category  TEXT NOT NULL CHECK (category IN ('physical', 'virtual')),
    icon      TEXT NOT NULL DEFAULT 'box',
    is_system BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Shared core model for physical and virtual assets.
CREATE TABLE assets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    hostname         TEXT NOT NULL DEFAULT '',
    type_id          UUID NOT NULL REFERENCES asset_types (id),
    status           TEXT NOT NULL DEFAULT 'unknown'
                     CHECK (status IN ('online', 'offline', 'maintenance', 'retired', 'unknown')),
    lifecycle        TEXT NOT NULL DEFAULT 'active'
                     CHECK (lifecycle IN ('planned', 'active', 'staged', 'decommissioned')),
    environment      TEXT NOT NULL DEFAULT 'production'
                     CHECK (environment IN ('production', 'development', 'testing', 'lab')),
    location_id      UUID REFERENCES locations (id) ON DELETE SET NULL,
    -- Deleting a host must never silently remove its guests (spec: no silent deletes).
    parent_id        UUID REFERENCES assets (id) ON DELETE RESTRICT,
    manufacturer     TEXT NOT NULL DEFAULT '',
    model            TEXT NOT NULL DEFAULT '',
    serial_number    TEXT NOT NULL DEFAULT '',
    os_name          TEXT NOT NULL DEFAULT '',
    os_version       TEXT NOT NULL DEFAULT '',
    cpu_model        TEXT NOT NULL DEFAULT '',
    cpu_cores        INTEGER,
    ram_gb           NUMERIC(10, 2),
    storage_gb       NUMERIC(12, 2),
    purchase_date    DATE,
    purchase_cost    NUMERIC(12, 2),
    warranty_expires DATE,
    notes            TEXT NOT NULL DEFAULT '',
    source           TEXT NOT NULL DEFAULT 'manual',
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assets_type ON assets (type_id);
CREATE INDEX idx_assets_status ON assets (status);
CREATE INDEX idx_assets_parent ON assets (parent_id);
CREATE INDEX idx_assets_location ON assets (location_id);
CREATE INDEX idx_assets_hostname ON assets (hostname);
CREATE INDEX idx_assets_serial ON assets (serial_number);

CREATE TABLE network_interfaces (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id   UUID NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'ethernet'
               CHECK (kind IN ('ethernet', 'wifi', 'vlan', 'bond', 'loopback', 'other')),
    mac_address TEXT,
    mtu        INTEGER,
    speed_mbps INTEGER,
    notes      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (asset_id, name)
);
CREATE INDEX idx_nic_asset ON network_interfaces (asset_id);
CREATE INDEX idx_nic_mac ON network_interfaces (mac_address);

CREATE TABLE asset_relationships (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_asset_id UUID NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    target_asset_id UUID NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('runs_on', 'connected_to', 'depends_on')),
    note            TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_asset_id, target_asset_id, kind),
    CHECK (source_asset_id <> target_asset_id)
);

CREATE TABLE network_contexts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    slug        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE vlan_definitions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    context_id  UUID NOT NULL REFERENCES network_contexts (id) ON DELETE CASCADE,
    vlan_id     INTEGER NOT NULL CHECK (vlan_id BETWEEN 1 AND 4094),
    name        TEXT NOT NULL,
    purpose     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (context_id, vlan_id)
);

CREATE TABLE subnets (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cidr           CIDR NOT NULL,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    context_id     UUID NOT NULL REFERENCES network_contexts (id),
    vlan_id        UUID REFERENCES vlan_definitions (id) ON DELETE SET NULL,
    gateway        INET,
    dns_servers    TEXT[] NOT NULL DEFAULT '{}',
    dhcp_start     INET,
    dhcp_end       INET,
    reserved_start INET,
    reserved_end   INET,
    location_id    UUID REFERENCES locations (id) ON DELETE SET NULL,
    environment    TEXT NOT NULL DEFAULT 'production'
                   CHECK (environment IN ('production', 'development', 'testing', 'lab')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Same network may exist in separate network contexts (VRF-style separation).
    UNIQUE (context_id, cidr)
);

CREATE TABLE ip_addresses (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subnet_id         UUID NOT NULL REFERENCES subnets (id) ON DELETE CASCADE,
    address           INET NOT NULL,
    status            TEXT NOT NULL DEFAULT 'available'
                      CHECK (status IN ('available', 'assigned', 'reserved', 'excluded', 'conflict')),
    assignment_method TEXT CHECK (assignment_method IN ('static', 'dhcp', 'reserved')),
    hostname          TEXT NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    interface_id      UUID REFERENCES network_interfaces (id) ON DELETE SET NULL,
    last_observed_at  TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Exactly one row per address per subnet: current state only.
    UNIQUE (subnet_id, address)
);
CREATE INDEX idx_ip_subnet ON ip_addresses (subnet_id);
CREATE INDEX idx_ip_address ON ip_addresses (address);
CREATE INDEX idx_ip_interface ON ip_addresses (interface_id);

-- Append-only assignment history; the active assignment is the row with released_at IS NULL.
CREATE TABLE ip_assignments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ip_address_id UUID NOT NULL REFERENCES ip_addresses (id) ON DELETE CASCADE,
    asset_id      UUID REFERENCES assets (id) ON DELETE SET NULL,
    interface_id  UUID REFERENCES network_interfaces (id) ON DELETE SET NULL,
    method        TEXT CHECK (method IN ('static', 'dhcp', 'reserved')),
    assigned_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at   TIMESTAMPTZ,
    assigned_by   UUID REFERENCES users (id) ON DELETE SET NULL,
    note          TEXT NOT NULL DEFAULT '',
    CONSTRAINT ip_assignments_release_after_assign
        CHECK (released_at IS NULL OR released_at >= assigned_at)
);
CREATE INDEX idx_ip_assign_ip ON ip_assignments (ip_address_id);
CREATE INDEX idx_ip_assign_asset ON ip_assignments (asset_id);
-- At most one active assignment per address row.
CREATE UNIQUE INDEX idx_ip_assign_active ON ip_assignments (ip_address_id) WHERE released_at IS NULL;

CREATE TABLE services (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL UNIQUE,
    description     TEXT NOT NULL DEFAULT '',
    category        TEXT NOT NULL DEFAULT 'other',
    deployment_type TEXT NOT NULL DEFAULT 'native'
                    CHECK (deployment_type IN ('native', 'docker', 'vm', 'lxc', 'kubernetes')),
    environment     TEXT NOT NULL DEFAULT 'production'
                    CHECK (environment IN ('production', 'development', 'testing', 'lab')),
    status          TEXT NOT NULL DEFAULT 'unknown'
                    CHECK (status IN ('online', 'offline', 'maintenance', 'retired', 'unknown')),
    repo_url        TEXT NOT NULL DEFAULT '',
    docs_url        TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE service_endpoints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id  UUID NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    label       TEXT NOT NULL DEFAULT '',
    protocol    TEXT NOT NULL DEFAULT 'http' CHECK (protocol IN ('http', 'https', 'tcp', 'udp')),
    port        INTEGER CHECK (port BETWEEN 1 AND 65535),
    url         TEXT NOT NULL DEFAULT '',
    is_external BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX idx_service_ep_service ON service_endpoints (service_id);

CREATE TABLE service_deployments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id      UUID NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    asset_id        UUID NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    deployment_type TEXT NOT NULL DEFAULT 'native'
                    CHECK (deployment_type IN ('native', 'docker', 'vm', 'lxc', 'kubernetes')),
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (service_id, asset_id)
);

CREATE TABLE tags (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    color      TEXT NOT NULL DEFAULT 'zinc',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Generic tagging across entity kinds; entity-side integrity is enforced in the service layer.
CREATE TABLE entity_tags (
    entity_type TEXT NOT NULL
                CHECK (entity_type IN ('asset', 'subnet', 'service', 'vlan', 'ip_address', 'location')),
    entity_id   UUID NOT NULL,
    tag_id      UUID NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (entity_type, entity_id, tag_id)
);
CREATE INDEX idx_entity_tags_tag ON entity_tags (tag_id);
CREATE INDEX idx_entity_tags_entity ON entity_tags (entity_type, entity_id);

CREATE TABLE integrations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         TEXT NOT NULL
                 CHECK (kind IN ('proxmox', 'docker', 'snmp', 'icmp', 'ssh', 'unifi', 'opnsense', 'pfsense', 'dns')),
    name         TEXT NOT NULL UNIQUE,
    description  TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT false,
    -- Holds secret references / env keys, never plaintext credentials.
    config       JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_sync_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, name)
);

CREATE TABLE integration_sync_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    integration_id UUID NOT NULL REFERENCES integrations (id) ON DELETE CASCADE,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    status         TEXT NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running', 'success', 'failed', 'partial')),
    summary        JSONB NOT NULL DEFAULT '{}'::jsonb,
    message        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sync_runs_integration ON integration_sync_runs (integration_id, started_at DESC);

CREATE TABLE audit_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_user_id UUID REFERENCES users (id) ON DELETE SET NULL,
    actor_label   TEXT NOT NULL DEFAULT '',
    entity_type   TEXT NOT NULL,
    entity_id     UUID,
    entity_label  TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    changes       JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_ip     INET
);
CREATE INDEX idx_audit_time ON audit_events (occurred_at DESC);
CREATE INDEX idx_audit_entity ON audit_events (entity_type, entity_id);

CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by UUID REFERENCES users (id) ON DELETE SET NULL
);
