-- Rebuild api_tokens in its final shape and keep every row. The column
-- added by the previous step carries a default; the table built here does
-- not, so a token cannot be stored without a name.
DROP INDEX IF EXISTS api_tokens_unique_name;

CREATE TABLE api_tokens_rebuilt (
    username TEXT NOT NULL CHECK (username != ''),
    prefix TEXT NOT NULL CHECK (prefix != ''),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, 
    expires_at DATETIME,
    scope TEXT,
    token TEXT NOT NULL,
    name TEXT NOT NULL,
    PRIMARY KEY (username, prefix)
) WITHOUT ROWID;

INSERT INTO api_tokens_rebuilt (username, prefix, created_at, expires_at, scope, token, name)
    SELECT username, prefix, created_at, expires_at, scope, token,
           CASE WHEN name IS NULL OR name = '' THEN prefix ELSE name END
    FROM api_tokens;

DROP TABLE api_tokens;
ALTER TABLE api_tokens_rebuilt RENAME TO api_tokens;

CREATE UNIQUE INDEX IF NOT EXISTS api_tokens_unique_name
    ON api_tokens (username, name);
