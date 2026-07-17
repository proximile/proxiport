package security

import (
	"testing"
	"time"
)

// TestMaxBadAttemptsRawCountBans documents that the original raw-count path
// (used by the API listener) is unchanged: N attempts from an IP ban it.
func TestMaxBadAttemptsRawCountBans(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(3, time.Hour, nil)
	const ip = "203.0.113.7"

	bl.AddBadAttempt(ip)
	bl.AddBadAttempt(ip)
	if bl.IsBanned(ip) {
		t.Fatal("must not be banned before reaching the threshold")
	}
	bl.AddBadAttempt(ip)
	if !bl.IsBanned(ip) {
		t.Fatal("must be banned once the threshold is reached")
	}
}

// TestDistinctSameCredentialNeverBans is the core of the self-ban fix: a client
// retrying the SAME wrong credential (its own reconnect loop) must never ban its
// own IP, no matter how many times it retries.
func TestDistinctSameCredentialNeverBans(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(3, time.Hour, nil)
	const ip = "203.0.113.7"

	for i := 0; i < 100; i++ {
		bl.AddDistinctBadAttempt(ip, "same-credential-fingerprint")
	}
	if bl.IsBanned(ip) {
		t.Fatal("retrying one wrong credential must never self-ban the IP")
	}
}

// TestDistinctCredentialsBan: an attacker trying enough DISTINCT credentials is
// still banned — brute-force protection is preserved.
func TestDistinctCredentialsBan(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(3, time.Hour, nil)
	const ip = "203.0.113.7"

	bl.AddDistinctBadAttempt(ip, "cred-a")
	bl.AddDistinctBadAttempt(ip, "cred-b")
	if bl.IsBanned(ip) {
		t.Fatal("two distinct credentials is below the threshold")
	}
	bl.AddDistinctBadAttempt(ip, "cred-c")
	if !bl.IsBanned(ip) {
		t.Fatal("three distinct credentials must ban")
	}
}

// TestDistinctDedupInterleaved: duplicates interleaved with new credentials only
// count once each; the ban triggers on the count of DISTINCT credentials.
func TestDistinctDedupInterleaved(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(3, time.Hour, nil)
	const ip = "203.0.113.7"

	bl.AddDistinctBadAttempt(ip, "a")
	bl.AddDistinctBadAttempt(ip, "a")
	bl.AddDistinctBadAttempt(ip, "b")
	bl.AddDistinctBadAttempt(ip, "b")
	bl.AddDistinctBadAttempt(ip, "a")
	if bl.IsBanned(ip) {
		t.Fatal("only two distinct credentials seen so far")
	}
	bl.AddDistinctBadAttempt(ip, "c")
	if !bl.IsBanned(ip) {
		t.Fatal("third distinct credential must ban")
	}
}

// TestSuccessClearsDistinctState: a successful auth clears the distinct-failure
// set and counter, so previously-seen credentials count afresh afterward.
func TestSuccessClearsDistinctState(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(3, time.Hour, nil)
	const ip = "203.0.113.7"

	bl.AddDistinctBadAttempt(ip, "a")
	bl.AddDistinctBadAttempt(ip, "b")
	bl.AddSuccessAttempt(ip)

	bl.AddDistinctBadAttempt(ip, "a")
	bl.AddDistinctBadAttempt(ip, "b")
	if bl.IsBanned(ip) {
		t.Fatal("counter must have reset on success")
	}
	bl.AddDistinctBadAttempt(ip, "c")
	if !bl.IsBanned(ip) {
		t.Fatal("must ban after three distinct credentials following the reset")
	}
}

// TestSuccessClearsBan: the method contract clears an existing ban on success.
func TestSuccessClearsBan(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(1, time.Hour, nil)
	const ip = "203.0.113.7"

	bl.AddBadAttempt(ip)
	if !bl.IsBanned(ip) {
		t.Fatal("threshold of 1 must ban immediately")
	}
	bl.AddSuccessAttempt(ip)
	if bl.IsBanned(ip) {
		t.Fatal("success must clear the ban")
	}
}

// TestDistinctPerVisitorIsolation: one IP's failures do not affect another's.
func TestDistinctPerVisitorIsolation(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(2, time.Hour, nil)
	const ipA, ipB = "203.0.113.7", "203.0.113.8"

	bl.AddDistinctBadAttempt(ipA, "a")
	bl.AddDistinctBadAttempt(ipA, "b") // ipA banned (2 distinct)
	bl.AddDistinctBadAttempt(ipB, "a") // ipB: 1 distinct

	if !bl.IsBanned(ipA) {
		t.Fatal("ipA must be banned")
	}
	if bl.IsBanned(ipB) {
		t.Fatal("ipB must not be banned by ipA's failures")
	}
}

// TestBanExpires: a ban lifts once its duration passes.
func TestBanExpires(t *testing.T) {
	bl := NewMaxBadAttemptsBanList(1, 20*time.Millisecond, nil)
	const ip = "203.0.113.7"

	bl.AddBadAttempt(ip)
	if !bl.IsBanned(ip) {
		t.Fatal("must be banned immediately")
	}
	time.Sleep(40 * time.Millisecond)
	if bl.IsBanned(ip) {
		t.Fatal("ban must expire after its duration")
	}
}
