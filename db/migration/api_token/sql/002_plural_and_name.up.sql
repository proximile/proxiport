ALTER TABLE api_token RENAME TO api_tokens;
-- A column that is NOT NULL needs a default to be added to a table that has
-- rows. Tokens from before names existed get their prefix as their name: it
-- is unique per user, as the name has to be for the index below.
ALTER TABLE api_tokens add name TEXT NOT NULL DEFAULT '';
UPDATE api_tokens SET name = prefix WHERE name = '';
CREATE UNIQUE INDEX api_tokens_unique_name
    ON api_tokens (username, name);
