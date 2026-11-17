-- Observed per-allocation facts from integrations (e.g. the MAC the ARP sweep
-- last saw answering at this address) live under metadata.observed.* and never
-- overwrite declared fields.

ALTER TABLE ip_addresses
    ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}';
