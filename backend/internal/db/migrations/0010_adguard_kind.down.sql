-- Remove the adguard kind. Existing adguard rows must be deleted first
-- (the check constraint would reject re-adding them).
DELETE FROM integrations WHERE kind = 'adguard';

ALTER TABLE integrations DROP CONSTRAINT IF EXISTS integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check
    CHECK (kind IN ('proxmox', 'docker', 'snmp', 'icmp', 'ssh', 'unifi', 'opnsense', 'pfsense', 'dns', 'cloudflare', 'kubernetes'));
