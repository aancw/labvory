-- Reference data: asset type catalogue (application configuration, not homelab seed data).
-- Homelab demo seed lives in internal/db/seed and is only applied with SEED_MODE=dev.

INSERT INTO asset_types (slug, label, category, icon) VALUES
    ('server',        'Server',             'physical', 'server'),
    ('desktop',       'Desktop',            'physical', 'monitor'),
    ('laptop',        'Laptop',             'physical', 'laptop'),
    ('mini-pc',       'Mini PC',            'physical', 'cpu'),
    ('router',        'Router',             'physical', 'router'),
    ('switch',        'Switch',             'physical', 'network'),
    ('firewall',      'Firewall',           'physical', 'shield'),
    ('access-point',  'Wireless AP',        'physical', 'wifi'),
    ('nas',           'NAS',                'physical', 'hard-drive'),
    ('sbc',           'SBC / Raspberry Pi', 'physical', 'circuit-board'),
    ('ups',           'UPS',                'physical', 'battery-charging'),
    ('iot',           'IoT Device',         'physical', 'radio'),
    ('other-physical','Other (Physical)',   'physical', 'box'),
    ('vm',            'Virtual Machine',    'virtual',  'monitor-dot'),
    ('lxc',           'LXC Container',      'virtual',  'container'),
    ('docker',        'Docker Container',   'virtual',  'container'),
    ('k8s-node',      'Kubernetes Node',    'virtual',  'boxes'),
    ('cloud-host',    'Cloud / Remote Host','virtual',  'cloud');

INSERT INTO network_contexts (name, slug, description) VALUES
    ('Default', 'default', 'Main routed network context');

INSERT INTO app_settings (key, value) VALUES
    ('instance.name', '"Lanvory"');
