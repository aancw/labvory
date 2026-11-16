-- ARP scan promotes discovered devices (one per MAC) into the inventory as
-- first-class assets; they need their own type.

INSERT INTO asset_types (slug, label, category, icon) VALUES
    ('discovered', 'Discovered Device', 'physical', 'radar')
ON CONFLICT (slug) DO NOTHING;
