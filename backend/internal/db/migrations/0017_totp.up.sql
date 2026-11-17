-- Two-factor authentication (RFC 6238 TOTP). The secret is stored encrypted
-- (AES-256-GCM via the secrets box); totp_enabled_at separates the pending
-- setup secret from an active one, and totp_last_step is the replay guard.

ALTER TABLE users
    ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '',
    ADD COLUMN totp_enabled_at TIMESTAMPTZ,
    ADD COLUMN totp_last_step BIGINT NOT NULL DEFAULT 0;
