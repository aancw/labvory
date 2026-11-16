-- Token purpose: "mcp" tokens authenticate only the MCP endpoint; "api" tokens
-- authenticate only the REST API (bearer). Existing tokens were MCP-era → 'mcp'.

ALTER TABLE api_tokens ADD COLUMN purpose TEXT NOT NULL DEFAULT 'mcp';

ALTER TABLE api_tokens ADD CONSTRAINT api_tokens_purpose_check CHECK (purpose IN ('api', 'mcp'));
