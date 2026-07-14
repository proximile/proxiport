package auditlog

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"time"

	"golang.org/x/crypto/hkdf"
)

// hmacInfo domain-separates the audit-chain HMAC key from every other use of the
// same DEK (field encryption, etc.), so the audit key is independent.
var hmacInfo = []byte("proxiport/auditlog/hmac/v1")

// deriveAuditHMACKey derives the audit-chain key from the data-encryption key.
// A nil/empty DEK yields a nil key, which disables tamper-evidence (the chain
// columns are written empty and verification is not available).
func deriveAuditHMACKey(dek []byte) ([]byte, error) {
	if len(dek) == 0 {
		return nil, nil
	}
	r := hkdf.New(sha256.New, dek, nil, hmacInfo)
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

// computeMAC returns the chained HMAC for an entry: HMAC(key, canonical) over an
// unambiguous, length-prefixed serialization of the sequence number, the
// previous row's MAC, and the entry's audited fields. The timestamp is bound as
// integer nanoseconds so it survives the database round-trip exactly.
func computeMAC(key []byte, e *Entry) string {
	mac := hmac.New(sha256.New, key)
	putUint64 := func(v uint64) {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		mac.Write(b[:])
	}
	writeField := func(s string) {
		putUint64(uint64(len(s)))
		mac.Write([]byte(s))
	}
	putUint64(uint64(e.Seq))                        //nolint:gosec // seq is a positive counter
	putUint64(uint64(e.Timestamp.UTC().UnixNano())) //nolint:gosec // wraparound is not a concern for a timestamp
	writeField(e.PrevMAC)
	writeField(e.Application)
	writeField(e.Action)
	writeField(e.Username)
	writeField(e.RemoteIP)
	writeField(e.ID)
	writeField(e.ClientID)
	writeField(e.ClientHostName)
	writeField(e.Request)
	writeField(e.Response)
	return hex.EncodeToString(mac.Sum(nil))
}

// ChainVerification is the result of walking the audit chain.
type ChainVerification struct {
	Enabled   bool      // whether a key was configured (chain present)
	Checked   int       // number of chained rows walked
	Valid     bool      // true if every chained row's MAC and link verified
	BreakSeq  int64     // the seq at which the chain first failed (0 if valid)
	BreakKind string    // "mac" (recomputed MAC mismatch) or "link" (prev_mac discontinuity) or "gap" (missing seq)
	OldestTS  time.Time // timestamp of the first chained row
}

// verifyChain recomputes the MAC of each chained row in seq order and checks
// that each row links to the previous one. Rows with seq == 0 are pre-chain
// (written before the migration or with no key) and are skipped. It returns at
// the first break so the caller can point at the tampered/removed row.
func verifyChain(key []byte, rows []*Entry) ChainVerification {
	res := ChainVerification{Enabled: len(key) > 0, Valid: true}
	if len(key) == 0 {
		return res
	}
	var prevMAC string
	var prevSeq int64
	started := false
	for _, e := range rows {
		if e.Seq == 0 {
			continue // pre-chain row
		}
		if !started {
			// Trust the genesis link of the first chained row we see; its own
			// MAC is still recomputed below.
			started = true
			res.OldestTS = e.Timestamp
		} else if e.Seq != prevSeq+1 {
			res.Valid, res.BreakSeq, res.BreakKind = false, e.Seq, "gap"
			return res
		} else if e.PrevMAC != prevMAC {
			res.Valid, res.BreakSeq, res.BreakKind = false, e.Seq, "link"
			return res
		}
		if computeMAC(key, e) != e.MAC {
			res.Valid, res.BreakSeq, res.BreakKind = false, e.Seq, "mac"
			return res
		}
		res.Checked++
		prevMAC = e.MAC
		prevSeq = e.Seq
	}
	return res
}
