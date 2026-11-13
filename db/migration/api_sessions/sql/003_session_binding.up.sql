-- Sessions get ids that are never issued twice (AUTOINCREMENT) and a
-- token_id that binds each session to the one token issued for it. Existing
-- sessions are dropped: their tokens carry no id the server has stored, so
-- every user signs in once more after this migration.
DROP TABLE IF EXISTS api_sessions;

CREATE TABLE api_sessions (
    session_id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
    expires_at DATETIME NOT NULL,
    username TEXT NOT NULL,
    last_access_at DATETIME NOT NULL,
    user_agent TEXT,
    ip_address TEXT,
    token_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_expires_at_time
	ON api_sessions (DATETIME(expires_at) DESC);

-- username may not be unique as the user may create new tokens before old ones are deleted/expired
CREATE INDEX idx_username
  ON api_sessions (username);
