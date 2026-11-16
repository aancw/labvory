-- ARP scan joins the connector catalogue (local L2 discovery: ip ↔ mac).

ALTER TABLE integrations DROP CONSTRAINT IF EXISTS integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check
    CHECK (kind IN ('proxmox', 'docker', 'snmp', 'icmp', 'ssh', 'unifi', 'opnsense', 'pfsense', 'dns', 'cloudflare', 'kubernetes', 'adguard', 'arp'));
