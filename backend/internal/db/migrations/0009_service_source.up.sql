-- Services can now be imported by integrations. `source` marks the owner:
-- empty = operator-declared (never overwritten by syncs), e.g. "kubernetes"
-- = imported/observed and refreshed on every sync.

ALTER TABLE services ADD COLUMN source TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_services_source ON services (source);
