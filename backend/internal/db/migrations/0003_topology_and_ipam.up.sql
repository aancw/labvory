-- Phase 5: manual physical connections carry interface/port metadata.
ALTER TABLE asset_relationships
    ADD COLUMN source_interface_id UUID REFERENCES network_interfaces (id) ON DELETE SET NULL,
    ADD COLUMN target_interface_id UUID REFERENCES network_interfaces (id) ON DELETE SET NULL,
    ADD COLUMN source_port TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_port TEXT NOT NULL DEFAULT '';

-- IPAM query hot paths.
CREATE INDEX idx_ip_subnet_status ON ip_addresses (subnet_id, status);
CREATE INDEX idx_ip_hostname ON ip_addresses (hostname);
CREATE INDEX idx_services_category ON services (category);
