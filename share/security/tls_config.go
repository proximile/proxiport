package security

import "crypto/tls"

func TLSConfig(configTLSMin string) *tls.Config {
	tlsMin := uint16(tls.VersionTLS13)
	switch configTLSMin {
	case "1.2":
		tlsMin = tls.VersionTLS12
	}

	// CurvePreferences is left unset on purpose: Go's default list of
	// key-exchange groups is kept current with the standard library and
	// puts the hybrid post-quantum group (X25519MLKEM768) first. A list set
	// here would freeze the choice at whatever was written down; the one
	// that used to be here allowed only the three NIST curves.
	// #nosec G402 -- disables G402: TLS MinVersion too low. (gosec)
	var TLSConfig = &tls.Config{
		MinVersion:               tlsMin,
		PreferServerCipherSuites: true,
	}
	return TLSConfig
}
