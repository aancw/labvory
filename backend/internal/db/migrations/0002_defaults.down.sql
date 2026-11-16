DELETE FROM app_settings WHERE key = 'instance.name';
DELETE FROM network_contexts WHERE slug = 'default';
DELETE FROM asset_types WHERE slug IN (
    'server', 'desktop', 'laptop', 'mini-pc', 'router', 'switch', 'firewall',
    'access-point', 'nas', 'sbc', 'ups', 'iot', 'other-physical',
    'vm', 'lxc', 'docker', 'k8s-node', 'cloud-host'
);
