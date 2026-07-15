package chshare

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"

	"github.com/jpillora/sizestr"
	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/backwardskey"
	"github.com/proximile/proxiport/share/logger"
)

// GenerateKey tries to stay compatible with go1.19 key generation
func GenerateKey(seed string) ([]byte, error) {
	var r io.Reader
	if seed == "" {
		r = rand.Reader
	} else {
		r = NewDetermRand([]byte(seed))
	}
	priv, err := useGo19CompatibleKeyGenerator(elliptic.P256(), r)
	if err != nil {
		return nil, err
	}
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("Unable to marshal ECDSA private key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), nil
}

func useGo19CompatibleKeyGenerator(curve elliptic.Curve, r io.Reader) (*ecdsa.PrivateKey, error) {
	if strings.HasPrefix(runtime.Version(), "go1.19") {
		return ecdsa.GenerateKey(curve, r)
	}
	return backwardskey.ECDSALegacy(curve, r)
}

// FingerprintKey returns the legacy MD5 colon-hex fingerprint of a host key
// (e.g. "36:98:...:15"). Kept for backward compatibility with agents pinned to
// this format; new deployments should pin the SHA-256 fingerprint instead.
func FingerprintKey(k ssh.PublicKey) string {
	bytes := md5.Sum(k.Marshal())
	strbytes := make([]string, len(bytes))
	for i, b := range bytes {
		strbytes[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(strbytes, ":")
}

// FingerprintKeySHA256 returns the SHA-256 fingerprint of a host key in the
// self-describing OpenSSH form "SHA256:<base64>". The "SHA256:" prefix lets a
// pin's algorithm be detected by shape, so MD5 and SHA-256 pins can coexist
// during the migration off MD5.
func FingerprintKeySHA256(k ssh.PublicKey) string {
	return ssh.FingerprintSHA256(k)
}

func HandleTCPStream(l *logger.Logger, connStats *ConnStats, src io.ReadWriteCloser, remote string) {
	dst, err := net.Dial("tcp", remote)
	if err != nil {
		l.Debugf("Remote failed (%s)", err)
		src.Close()
		return
	}
	connStats.Open()
	l.Debugf("%s: Open", connStats)
	s, r := Pipe(src, dst)
	connStats.Close()
	l.Debugf("%s: Close (sent %s received %s)", connStats, sizestr.ToString(s), sizestr.ToString(r))
}
