ALTER TABLE users
    DROP COLUMN totp_last_step,
    DROP COLUMN totp_enabled_at,
    DROP COLUMN totp_secret;
