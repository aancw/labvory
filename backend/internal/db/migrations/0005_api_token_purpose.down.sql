ALTER TABLE api_tokens DROP CONSTRAINT IF EXISTS api_tokens_purpose_check;
ALTER TABLE api_tokens DROP COLUMN IF EXISTS purpose;
