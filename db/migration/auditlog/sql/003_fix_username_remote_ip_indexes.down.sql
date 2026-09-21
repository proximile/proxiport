-- Only the indexes this migration created are dropped. The two it replaced were
-- indexes over constant expressions; recreating them would restore a defect
-- rather than a capability.
DROP INDEX IF EXISTS "auditlog_username_timestamp";
DROP INDEX IF EXISTS "auditlog_remote_ip";
