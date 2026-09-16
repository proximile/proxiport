package models

// The protocol vocabulary for tunnels. The constants live in remote.go, next to
// the Remote they describe; the check lives here because it is the one place
// the server asks "is this string a protocol at all?" before using it as a map
// key or handing it to a syscall wrapper.

// IsValidProtocol reports whether p is a protocol a tunnel can actually use.
// The Protocol field arrives raw from the agent's JSON connection request --
// sanitizeAgentRemotes deliberately leaves it alone -- so anything that indexes
// a per-protocol structure with it must check here first. An empty string is
// not valid: only the "host:port/proto" string parser defaults to tcp, and a
// decoded remote that omits the field has no protocol at all.
func IsValidProtocol(p string) bool {
	switch p {
	case ProtocolTCP, ProtocolUDP, ProtocolTCPUDP:
		return true
	default:
		return false
	}
}
