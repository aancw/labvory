DROP INDEX IF EXISTS idx_services_category;
DROP INDEX IF EXISTS idx_ip_hostname;
DROP INDEX IF EXISTS idx_ip_subnet_status;

ALTER TABLE asset_relationships
    DROP COLUMN IF EXISTS source_port,
    DROP COLUMN IF EXISTS target_port,
    DROP COLUMN IF EXISTS source_interface_id,
    DROP COLUMN IF EXISTS target_interface_id;
