-- Remove the discovered device type. Assets must be deleted first (FK).
DELETE FROM assets WHERE type_id = (SELECT id FROM asset_types WHERE slug = 'discovered');
DELETE FROM asset_types WHERE slug = 'discovered';
