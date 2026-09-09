package clienttunnel

import (
	"context"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

type TunnelProtocol interface {
	Start(ctx context.Context) error
	Terminate(force bool) error
	LastActive() time.Time
	SetACL(*TunnelACL)
}

type MultiProtocolTunnel struct {
	Protocols []TunnelProtocol
}

func (mt *MultiProtocolTunnel) Start(ctx context.Context) error {
	started := make([]TunnelProtocol, 0, len(mt.Protocols))
	for _, tp := range mt.Protocols {
		if err := tp.Start(ctx); err != nil {
			// Roll back the protocols already started so a partial start (e.g.
			// TCP bound, then the UDP bind fails) does not leave an orphaned
			// listener/port bound until the client context is canceled at reap.
			for _, s := range started {
				_ = s.Terminate(true)
			}
			return err
		}
		started = append(started, tp)
	}
	return nil
}

func (mt *MultiProtocolTunnel) Terminate(force bool) error {
	var result error
	for _, tp := range mt.Protocols {
		err := tp.Terminate(force)
		if err != nil {
			result = multierror.Append(result, err)
		}
	}
	return result
}

func (mt *MultiProtocolTunnel) LastActive() time.Time {
	var result time.Time
	for _, tp := range mt.Protocols {
		v := tp.LastActive()
		if v.After(result) {
			result = v
		}
	}
	return result
}

func (mt *MultiProtocolTunnel) SetACL(acl *TunnelACL) {
	for _, tp := range mt.Protocols {
		tp.SetACL(acl)
	}
}

// TODO(m-terel): Refactor to use separate models for representation and business logic.
// Tunnel represents active remote proxy connection
type Tunnel struct {
	ID string `json:"id"`

	models.Remote

	TunnelProtocol      `json:"-"`
	InternalTunnelProxy *InternalTunnelProxy `json:"-"`
	CreatedAt           time.Time            `json:"created_at"`
}

// Start brings up the tunnel's live protocol handlers.
//
// Same hazard as Terminate, and the same nil TunnelProtocol on any Tunnel
// restored from storage — but the safe answer here is the opposite one.
// Reporting success would leave the caller believing a listener exists when
// nothing is bound; a stored record describes a tunnel, it is not one, and the
// rebuild path constructs a fresh Tunnel with NewTunnel before starting it.
// Fail closed and say so.
func (t *Tunnel) Start(ctx context.Context) error {
	if t.TunnelProtocol == nil {
		return errors.New("tunnel has no live protocol: a record restored from storage must be rebuilt with NewTunnel before it can be started")
	}
	return t.TunnelProtocol.Start(ctx)
}

// Terminate stops the tunnel's live protocol handlers.
//
// TunnelProtocol is an interface field tagged `json:"-"`, so a Tunnel restored
// from client storage has none: the record survives a daemon restart but the
// listeners it describes do not. The promoted TunnelProtocol.Terminate would
// then be a call through a nil interface, which panics. That is not an edge
// case — it is the state of every stored tunnel after a restart, and the
// reconnect path terminates them before rebuilding, so the first client with a
// stored tunnel to come back would crash its own connection handler and could
// never reconnect.
//
// A record with no live handlers has nothing to stop, so report success.
func (t *Tunnel) Terminate(force bool) error {
	if t.TunnelProtocol == nil {
		return nil
	}
	return t.TunnelProtocol.Terminate(force)
}

// LastActive reports when the tunnel last carried traffic.
//
// Same hazard as Terminate: the promoted TunnelProtocol.LastActive panics on a
// Tunnel restored from storage. Nothing reaches it that way today — the
// idle-timeout goroutines only ever hold tunnels they just started — but that
// is an invariant asserted in another file, which is exactly what made the
// Terminate case a fleet-wide outage rather than a caught bug.
//
// CreatedAt is the honest answer for a record with no live handlers: it is the
// only activity timestamp that survives storage, so an idle-timeout comparison
// against it stays sane, where the zero time would read as "idle since year 1".
func (t *Tunnel) LastActive() time.Time {
	if t.TunnelProtocol == nil {
		return t.CreatedAt
	}
	return t.TunnelProtocol.LastActive()
}

// SetACL applies an access list to the tunnel's live protocol handlers.
//
// Same hazard again. A record with no handlers has nothing to apply an ACL to;
// the stored Remote.ACL is what a rebuilt tunnel is constructed from, and that
// is set by the caller independently of this method.
func (t *Tunnel) SetACL(acl *TunnelACL) {
	if t.TunnelProtocol == nil {
		return
	}
	t.TunnelProtocol.SetACL(acl)
}

func NewTunnel(logger *logger.Logger, ssh ssh.Conn, id string, remote models.Remote, acl *TunnelACL) (*Tunnel, error) {
	logger = logger.Fork("tunnel#%s:%s", id, remote)
	logger.Debugf("new tunnel with remote = %#v", remote)

	var tunnelProtocol TunnelProtocol
	switch remote.Protocol {
	case models.ProtocolUDP:
		tunnelProtocol = newTunnelUDP(logger, ssh, remote, acl)
	case models.ProtocolTCP:
		tunnelProtocol = newTunnelTCP(logger, ssh, remote, acl)
	case models.ProtocolTCPUDP:
		tunnelProtocol = &MultiProtocolTunnel{
			Protocols: []TunnelProtocol{
				newTunnelTCP(logger, ssh, remote, acl),
				newTunnelUDP(logger, ssh, remote, acl),
			},
		}
	default:
		return nil, errors.Errorf("unsupported protocol %q", remote.Protocol)
	}

	return &Tunnel{
		Remote:         remote,
		ID:             id,
		TunnelProtocol: tunnelProtocol,
		CreatedAt:      time.Now(),
	}, nil
}
