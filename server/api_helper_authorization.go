package chserver

import (
	"net/http"

	"github.com/proximile/proxiport/server/bearer"
	chshare "github.com/proximile/proxiport/share"
)

// loginBanKey scopes a username-keyed throttle to the source address. Keying on
// the username alone let any third party lock a victim out of the API simply by
// spraying bad credentials at their name.
func loginBanKey(remoteIP, username string) string {
	return remoteIP + "|" + username
}

// handleBannedIPs records a failed authentication attempt against the per-IP ban
// list.
//
// It deliberately does not clear the counter when authorized is true. Clearing
// it from the auth middleware meant that every authorized request reset the
// limiter — including each request in a /verify-2fa loop, which carries a valid
// 2FA-pending bearer and is therefore "authorized" before the code is even
// checked. The limiter never engaged and TOTP could be brute-forced by anyone
// holding the password. The counter is cleared only by recordAuthSuccess, at the
// single point where a full session is minted.
func (al *APIListener) handleBannedIPs(r *http.Request, authorized bool) (ok bool) {
	if al.bannedIPs != nil && !authorized {
		al.bannedIPs.AddBadAttempt(chshare.RemoteIP(r))
	}

	return true
}

// recordAuthSuccess clears the per-IP failure counter after a completed
// authentication: the password when 2FA is off, or the second factor when it is
// on. Both paths converge on sendJWTToken.
func (al *APIListener) recordAuthSuccess(r *http.Request) {
	if al.bannedIPs != nil {
		al.bannedIPs.AddSuccessAttempt(chshare.RemoteIP(r))
	}
}

// recordFailedAuth counts a rejected API request toward the per-IP ban.
//
// A rejected bearer token is counted once per distinct token rather than once
// per request. A browser holding an expired or revoked session fires several
// requests in parallel before it notices the session is gone, and counting each
// of them let one stale session ban its own address after a few page loads.
// A caller cycling through different tokens is still counted for every one,
// and failed basic-auth passwords and anonymous requests count every time.
func (al *APIListener) recordFailedAuth(r *http.Request, isBearerOnly bool) {
	if al.bannedIPs == nil {
		return
	}
	if !isBearerOnly {
		if _, _, basicAuthProvided := r.BasicAuth(); basicAuthProvided {
			al.bannedIPs.AddBadAttempt(chshare.RemoteIP(r))
			return
		}
	}
	if token, ok := bearer.GetBearerToken(r); ok {
		al.bannedIPs.AddDistinctBadAttempt(chshare.RemoteIP(r), credentialFingerprint("bearer", []byte(token)))
		return
	}
	al.bannedIPs.AddBadAttempt(chshare.RemoteIP(r))
}
