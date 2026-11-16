DELETE FROM asset_types WHERE slug = 'pod';

ALTER TABLE integrations DROP CONSTRAINT IF EXISTS integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check
    CHECK (kind IN ('proxmox', 'docker', 'snmp', 'icmp', 'ssh', 'unifi', 'opnsense', 'pfsense', 'dns', 'cloudflare'));
