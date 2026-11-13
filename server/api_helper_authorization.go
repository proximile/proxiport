package chserver

import (
	"net"
	"net/http"
	"strconv"

	"github.com/proximile/proxiport/server/bearer"
	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/security"
)

// loginBanKey scopes a username-keyed throttle to the source address. Keying on
// the username alone let any third party lock a victim out of the API simply by
// spraying bad credentials at their name.
func loginBanKey(remoteIP, username string) string {
	return remoteIP + "|" + username
}

// isLoopbackAddr reports whether a caller's address is the server's own host.
//
// Such a caller is a relay in front of the API: a Tor onion service mapped to
// 127.0.0.1, or a reverse proxy on the same host that is not listed in
// trusted_proxies. Everyone who arrives through it has this one address, so a
// ban of the address locks all of them out, and a handful of anonymous
// requests is enough to cause it.
func isLoopbackAddr(remoteIP string) bool {
	ip := net.ParseIP(remoteIP)
	return ip != nil && ip.IsLoopback()
}

// loopbackUserKey is the ban-list key of a user name for loopback callers. It
// also is what the ban list writes to the log.
func loopbackUserKey(username string) string {
	return "user " + strconv.Quote(username) + " via a loopback address"
}

// banTarget returns the list and the key a failed authentication counts
// against, or a nil list when it is not counted.
//
// A caller with an address of its own is counted by that address, whatever the
// request named. A loopback caller is counted by the user name the attempt was
// made for, so that guessing a password or a second factor stays bounded
// without one caller being able to lock out everyone else behind the relay.
// A loopback request that names nobody (anonymous, or a rejected bearer token)
// is not counted: there is no secret it could be guessing at a useful rate.
func (al *APIListener) banTarget(remoteIP, username string) (*security.MaxBadAttemptsBanList, string) {
	if !isLoopbackAddr(remoteIP) {
		return al.bannedIPs, remoteIP
	}
	if username == "" {
		return nil, ""
	}
	return al.bannedLoopbackUsers, loopbackUserKey(username)
}

// loopbackUserBanned reports whether logins for username are refused from
// loopback because of too many failures there.
func (al *APIListener) loopbackUserBanned(remoteIP, username string) bool {
	return al.bannedLoopbackUsers != nil && username != "" && isLoopbackAddr(remoteIP) &&
		al.bannedLoopbackUsers.IsBanned(loopbackUserKey(username))
}

// loginThrottled reports whether an authentication attempt for username must
// be refused before its credential is looked at: the short per-user wait after
// a failure, or the ban of that user name for loopback callers.
func (al *APIListener) loginThrottled(remoteIP, username string) bool {
	return al.bannedUsers.IsBanned(loginBanKey(remoteIP, username)) || al.loopbackUserBanned(remoteIP, username)
}

// handleBannedIPs records a failed authentication attempt against the ban list
// (see banTarget). username is the name the attempt was made for, empty when
// the request named nobody.
//
// It deliberately does not clear the counter when authorized is true. Clearing
// it from the auth middleware meant that every authorized request reset the
// limiter — including each request in a /verify-2fa loop, which carries a valid
// 2FA-pending bearer and is therefore "authorized" before the code is even
// checked. The limiter never engaged and TOTP could be brute-forced by anyone
// holding the password. The counter is cleared only by recordAuthSuccess, at the
// single point where a full session is minted.
func (al *APIListener) handleBannedIPs(r *http.Request, authorized bool, username string) (ok bool) {
	if !authorized {
		if list, key := al.banTarget(chshare.RemoteIP(r), username); list != nil {
			list.AddBadAttempt(key)
		}
	}

	return true
}

// recordAuthSuccess clears the failure counter after a completed
// authentication: the password when 2FA is off, or the second factor when it is
// on. Both paths converge on sendJWTToken.
func (al *APIListener) recordAuthSuccess(r *http.Request, username string) {
	if list, key := al.banTarget(chshare.RemoteIP(r), username); list != nil {
		list.AddSuccessAttempt(key)
	}
}

// recordFailedAuth counts a rejected API request toward the ban (see
// banTarget). username is the name a basic-auth attempt was made for.
//
// A rejected bearer token is counted once per distinct token rather than once
// per request. A browser holding an expired or revoked session fires several
// requests in parallel before it notices the session is gone, and counting each
// of them let one stale session ban its own address after a few page loads.
// A caller cycling through different tokens is still counted for every one,
// and failed basic-auth passwords and anonymous requests count every time.
// From loopback only the failed basic-auth passwords count, per user name.
func (al *APIListener) recordFailedAuth(r *http.Request, isBearerOnly bool, username string) {
	remoteIP := chshare.RemoteIP(r)
	if !isBearerOnly {
		if _, _, basicAuthProvided := r.BasicAuth(); basicAuthProvided {
			if list, key := al.banTarget(remoteIP, username); list != nil {
				list.AddBadAttempt(key)
			}
			return
		}
	}
	list, key := al.banTarget(remoteIP, "")
	if list == nil {
		return
	}
	if token, ok := bearer.GetBearerToken(r); ok {
		list.AddDistinctBadAttempt(key, credentialFingerprint("bearer", []byte(token)))
		return
	}
	list.AddBadAttempt(key)
}
