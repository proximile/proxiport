// Package e2e runs a small SSH server on the agent's loopback interface so an
// operator can reach hosts behind the agent with the ProxiPort server unable to
// read the traffic.
//
// The operator opens an ordinary ProxiPort tunnel to this server's loopback
// address and then runs a standard SSH client through that tunnel, pinning this
// server's host key. The ProxiPort server relays the SSH session but holds
// neither the host key nor the operator's key, so it can neither read the
// session nor impersonate either end. Only port forwarding (direct-tcpip) is
// offered; shells, exec and reverse forwards are refused.
package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/logger"
)

const (
	handshakeTimeout = 10 * time.Second
	dialTimeout      = 10 * time.Second
)

// Config is what the server needs to run.
type Config struct {
	// Listen is the loopback address to accept connections on.
	Listen string
	// HostKeyFile holds the server's ed25519 private key. It is created on
	// first use.
	HostKeyFile string
	// AuthorizedKeysFile lists the operator public keys allowed to connect, in
	// OpenSSH authorized_keys format. It is read on every login attempt, so
	// keys can be added or removed without a restart.
	AuthorizedKeysFile string
	// Allowed reports whether a forward to host:port is permitted.
	Allowed func(remote string) bool
}

// Server is the agent-side SSH endpoint.
type Server struct {
	cfg      Config
	logger   *logger.Logger
	sshCfg   *ssh.ServerConfig
	hostFP   string
	listener net.Listener
	wg       sync.WaitGroup
}

// ValidateListen requires a loopback address with an explicit port. The
// server is meant to be reached only through a ProxiPort tunnel, never
// directly from the network.
func ValidateListen(listen string) error {
	if err := checkLoopback(listen); err != nil {
		return err
	}
	_, port, _ := net.SplitHostPort(listen)
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("invalid listen port in %q", listen)
	}
	return nil
}

func checkLoopback(listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", listen, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address %q must be a loopback IP such as 127.0.0.1", listen)
	}
	return nil
}

// LoadOrCreateHostKey returns the host key stored at path, generating and
// saving a new ed25519 key if the file does not exist.
func LoadOrCreateHostKey(path string) (ssh.Signer, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the path is operator configuration
	if errors.Is(err, os.ErrNotExist) {
		return createHostKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading host key: %w", err)
	}
	if err := checkPrivate(path); err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("parsing host key %s: %w", path, err)
	}
	return signer, nil
}

func createHostKey(path string) (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "proxiport e2e host key")
	if err != nil {
		return nil, err
	}
	// O_EXCL: never overwrite a key that appeared since the read above.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the path is operator configuration
	if err != nil {
		return nil, fmt.Errorf("creating host key: %w", err)
	}
	if err := pem.Encode(f, block); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing host key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("writing host key: %w", err)
	}
	return ssh.NewSignerFromKey(priv)
}

// checkPrivate refuses a host key that other users can read: whoever reads it
// can impersonate this agent to the operator.
func checkPrivate(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("host key %s is accessible by other users (mode %04o); chmod 600 it", path, st.Mode().Perm())
	}
	return nil
}

// Fingerprint returns the SHA-256 fingerprint and public key of the existing
// host key at path. It does not create one: the agent does that on start, as
// the user that has to own it.
func Fingerprint(path string) (string, ssh.PublicKey, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the path is operator configuration
	if err != nil {
		return "", nil, fmt.Errorf("reading host key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return "", nil, fmt.Errorf("parsing host key %s: %w", path, err)
	}
	return ssh.FingerprintSHA256(signer.PublicKey()), signer.PublicKey(), nil
}

// New prepares a server. It loads (or creates) the host key and checks that
// the authorized keys file can be read, so a misconfiguration fails at startup.
func New(cfg Config, l *logger.Logger) (*Server, error) {
	if err := checkLoopback(cfg.Listen); err != nil {
		return nil, err
	}
	if cfg.Allowed == nil {
		return nil, errors.New("e2e: no forward policy")
	}
	signer, err := LoadOrCreateHostKey(cfg.HostKeyFile)
	if err != nil {
		return nil, err
	}
	if _, err := readAuthorizedKeys(cfg.AuthorizedKeysFile); err != nil {
		return nil, err
	}

	s := &Server{cfg: cfg, logger: l, hostFP: ssh.FingerprintSHA256(signer.PublicKey())}
	s.sshCfg = &ssh.ServerConfig{
		MaxAuthTries:      3,
		PublicKeyCallback: s.checkPublicKey,
		ServerVersion:     "SSH-2.0-ProxiPort-e2e",
	}
	s.sshCfg.AddHostKey(signer)
	return s, nil
}

// HostKeyFingerprint is the fingerprint operators pin.
func (s *Server) HostKeyFingerprint() string {
	return s.hostFP
}

func readAuthorizedKeys(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the path is operator configuration
	if err != nil {
		return nil, fmt.Errorf("reading authorized keys: %w", err)
	}
	keys := map[string]bool{}
	for len(b) > 0 {
		pub, _, _, rest, err := ssh.ParseAuthorizedKey(b)
		if err != nil {
			// ParseAuthorizedKey skips blank and comment lines itself, so
			// this is the end of the file or a line it cannot parse.
			break
		}
		keys[string(pub.Marshal())] = true
		b = rest
	}
	return keys, nil
}

func (s *Server) checkPublicKey(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	keys, err := readAuthorizedKeys(s.cfg.AuthorizedKeysFile)
	if err != nil {
		s.logger.Errorf("%v", err)
		return nil, errors.New("unauthorized")
	}
	if !keys[string(key.Marshal())] {
		s.logger.Infof("rejected key %s for user %q", ssh.FingerprintSHA256(key), meta.User())
		return nil, errors.New("unauthorized")
	}
	return &ssh.Permissions{Extensions: map[string]string{"key": ssh.FingerprintSHA256(key)}}, nil
}

// Start begins accepting connections. It returns once the listener is open.
func (s *Server) Start(ctx context.Context) error {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("e2e listen: %w", err)
	}
	s.listener = l
	s.logger.Infof("listening on %s, host key %s", l.Addr(), s.HostKeyFingerprint())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		_ = l.Close()
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(ctx)
	}()
	return nil
}

// Addr is the address the server listens on, once started.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Wait blocks until the server has stopped after its context is canceled.
func (s *Server) Wait() {
	s.wg.Wait()
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Errorf("accept: %v", err)
			}
			return
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	sconn, chans, reqs, err := ssh.NewServerConn(conn, s.sshCfg)
	if err != nil {
		s.logger.Debugf("handshake failed: %v", err)
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	l := s.logger.Fork("%s", sconn.Permissions.Extensions["key"])
	l.Infof("session opened")

	go func() {
		<-ctx.Done()
		_ = sconn.Close()
	}()
	// Global requests are tcpip-forward and keepalives: reverse forwards are
	// not offered, and DiscardRequests answers every request with a refusal.
	go ssh.DiscardRequests(reqs)

	for nc := range chans {
		if nc.ChannelType() != "direct-tcpip" {
			_ = nc.Reject(ssh.UnknownChannelType, "only port forwarding is supported")
			continue
		}
		go s.handleDirectTCPIP(l, nc)
	}
	l.Infof("session closed")
}

// directTCPIP is the RFC 4254 section 7.2 channel payload.
type directTCPIP struct {
	Host     string
	Port     uint32
	OrigHost string
	OrigPort uint32
}

func (s *Server) handleDirectTCPIP(l *logger.Logger, nc ssh.NewChannel) {
	var req directTCPIP
	if err := ssh.Unmarshal(nc.ExtraData(), &req); err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, "malformed forward request")
		return
	}
	if req.Port == 0 || req.Port > 65535 {
		_ = nc.Reject(ssh.ConnectionFailed, "invalid port")
		return
	}
	remote := net.JoinHostPort(req.Host, strconv.FormatUint(uint64(req.Port), 10))
	if !s.cfg.Allowed(remote) {
		l.Infof("forward to %s refused by tunnel_allowed", remote)
		_ = nc.Reject(ssh.Prohibited, "not allowed by tunnel_allowed")
		return
	}

	dst, err := net.DialTimeout("tcp", remote, dialTimeout)
	if err != nil {
		l.Debugf("forward to %s failed: %v", remote, err)
		_ = nc.Reject(ssh.ConnectionFailed, "connection failed")
		return
	}
	ch, chReqs, err := nc.Accept()
	if err != nil {
		_ = dst.Close()
		return
	}
	go ssh.DiscardRequests(chReqs)

	l.Debugf("forward to %s opened", remote)
	sent, received := chshare.Pipe(ch, dst)
	l.Debugf("forward to %s closed (sent %d, received %d bytes)", remote, sent, received)
}
