DROP TABLE IF EXISTS api_sessions;

CREATE TABLE api_sessions (
    session_id INTEGER PRIMARY KEY NOT NULL,
    expires_at DATETIME NOT NULL,
    username TEXT NOT NULL,
    last_access_at DATETIME NOT NULL,
    user_agent TEXT,
    ip_address TEXT
);

CREATE INDEX idx_expires_at_time
	ON api_sessions (DATETIME(expires_at) DESC);

CREATE INDEX idx_username
  ON api_sessions (username);
