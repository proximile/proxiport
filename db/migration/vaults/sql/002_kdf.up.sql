-- Self-describing key-derivation descriptor (algorithm, cost params, salt).
-- NOT NULL so existing rows backfill to '' (the legacy marker) rather than NULL;
-- a vault with an empty kdf is re-keyed to Argon2id on the next successful unlock.
ALTER TABLE `status` ADD COLUMN `kdf` TEXT NOT NULL DEFAULT '';

-- Drop the stored plaintext verifier: it was a known-plaintext oracle that let a
-- host adversary brute-force the passphrase offline. Verification now relies on
-- the AES-GCM authentication tag of the enc_check ciphertext alone.
ALTER TABLE `status` DROP COLUMN `dec_check`;
