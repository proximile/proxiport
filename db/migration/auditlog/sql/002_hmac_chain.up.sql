-- Tamper-evidence: a per-entry keyed HMAC chained to the prior row's MAC.
-- seq is a monotonic sequence assigned at write time; prev_mac is the MAC of
-- the previous row (empty for the genesis / first row after this migration);
-- mac is HMAC(key, seq || prev_mac || canonical(row fields)). Verification walks
-- the chain and recomputes each MAC. Existing rows predate the chain and carry
-- empty mac/prev_mac (marked pre-chain by seq = 0).
ALTER TABLE "auditlog" ADD COLUMN "seq" INTEGER NOT NULL DEFAULT 0;
ALTER TABLE "auditlog" ADD COLUMN "mac" TEXT NOT NULL DEFAULT '';
ALTER TABLE "auditlog" ADD COLUMN "prev_mac" TEXT NOT NULL DEFAULT '';
