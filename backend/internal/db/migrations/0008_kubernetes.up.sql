-- Kubernetes joins the connector catalogue; pods become an asset type.

ALTER TABLE integrations DROP CONSTRAINT IF EXISTS integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check
    CHECK (kind IN ('proxmox', 'docker', 'snmp', 'icmp', 'ssh', 'unifi', 'opnsense', 'pfsense', 'dns', 'cloudflare', 'kubernetes'));

INSERT INTO asset_types (slug, label, category, icon) VALUES
    ('pod', 'Kubernetes Pod', 'virtual', 'container')
ON CONFLICT (slug) DO NOTHING;
