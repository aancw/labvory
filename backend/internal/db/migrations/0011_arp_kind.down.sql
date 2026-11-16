-- Remove the arp kind. Existing arp rows must be deleted first.
DELETE FROM integrations WHERE kind = 'arp';

ALTER TABLE integrations DROP CONSTRAINT IF EXISTS integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check
    CHECK (kind IN ('proxmox', 'docker', 'snmp', 'icmp', 'ssh', 'unifi', 'opnsense', 'pfsense', 'dns', 'cloudflare', 'kubernetes', 'adguard'));
