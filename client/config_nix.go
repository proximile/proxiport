//go:build !windows
// +build !windows

package chclient

// DefaultDataDir is the agent's own state directory, and is deliberately not
// the server's /var/lib/proxiport.
//
// The two used to share it, along with a single `proxiport` uid. The agent
// executes operator-supplied commands as its own uid, so that put it inside
// the server's trust boundary on any host carrying both: read of
// proxiportd.conf (jwt_secret, key_seed, the admin credential) and read/write
// on every database, the vault and the ACME key cache. Separate accounts with
// a shared directory would not have been a separation, so the directory moved
// with the account.
const DefaultDataDir = "/var/lib/proxiport-agent"
