-- Planned maintenance window per service endpoint. While it is in the future
-- the scheduler still probes the endpoint but suppresses down/recovered
-- notifications and the dashboard "services down" alert excludes it.
ALTER TABLE service_endpoints ADD COLUMN maintenance_until TIMESTAMPTZ;
