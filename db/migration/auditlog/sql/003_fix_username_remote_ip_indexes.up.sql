-- 001 built "auditlog_username" over "client_username" and "auditlog_remote_ip"
-- over "client_remote_ip". Neither column has ever existed on this table: the
-- columns are "username" and "remote_ip". SQLite's double-quoted-string
-- fallback -- SQLITE_DQS defaults to 3 in the amalgamation this project builds,
-- so an unresolvable double-quoted identifier is silently reinterpreted as a
-- string literal -- let each CREATE INDEX succeed and produce an index over a
-- constant expression. Maintained on every insert, useful to no query.
--
-- The cost is not theoretical. AuditLog.List force-appends `username = <caller>`
-- to every request from a non-admin, and both the List and the Count then run
-- that predicate as a full table scan over a month of audit rows -- on a
-- database opened with a single connection, which is the same connection the
-- audit write path needs.
--
-- username is indexed together with timestamp because that is how it is always
-- queried: the forced username filter plus the listing's timestamp ordering.
DROP INDEX IF EXISTS "auditlog_username";
DROP INDEX IF EXISTS "auditlog_remote_ip";

CREATE INDEX "auditlog_username_timestamp" ON "auditlog" (
    "username" ASC,
    "timestamp" DESC
);

CREATE INDEX "auditlog_remote_ip" ON "auditlog" (
    "remote_ip" ASC
);
