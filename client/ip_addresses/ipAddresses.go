package ipAddresses

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/comm"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/myip"
)

type Fetcher struct {
	// mtx protects conn and current.
	mtx  sync.RWMutex
	conn ssh.Conn
	// current is the last set of addresses the server was successfully told
	// about. It used to be a package-level var, which meant the state of one
	// fetcher was shared with every other one and with every test in the
	// process; it is per-fetcher state and now lives with the fetcher. It is a
	// value, not a pointer, so a zero Fetcher behaves like a fresh one.
	current     models.IPAddresses
	logger      *logger.Logger
	loopWait    time.Duration
	IPAPIURL    string
	refreshChan chan struct{}

	// fetchIPs is myip.GetMyIPs. It is a field so that a test can hold the
	// lookup open across the exact moment Stop runs -- the window this code
	// exists to survive, and one that is otherwise only reachable by timing a
	// real network round trip. Nothing in production replaces it.
	fetchIPs func(ctx context.Context, apiURL string) (*models.IPAddresses, error)
}

func NewFetcher(logger *logger.Logger, IPAPIURL string, loopWait time.Duration) *Fetcher {
	return &Fetcher{
		logger:   logger,
		IPAPIURL: IPAPIURL,
		loopWait: loopWait,
		fetchIPs: myip.GetMyIPs,
	}
}

// getConn returns the connection under the read lock, so a caller can never
// observe a torn or stale value while Stop is running.
func (i *Fetcher) getConn() ssh.Conn {
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	return i.conn
}

func (i *Fetcher) sendIPAddresses(ctx context.Context) {
	// The early check is a cheap way to skip the HTTP round trips when there is
	// no connection at all. It is not the guard that matters: the connection can
	// -- and on a hostile or flapping server, does -- go away while the fetch
	// below is in flight, which takes up to two seconds per address family.
	if i.getConn() == nil {
		return
	}

	fetch := i.fetchIPs
	if fetch == nil {
		fetch = myip.GetMyIPs
	}

	ips, err := fetch(ctx, i.IPAPIURL)
	if err != nil {
		i.logger.Errorf("Failed to determine IP addresses: %s", err)
		return
	}

	i.mtx.Lock()
	defer i.mtx.Unlock()

	// Re-read the connection now that the fetch has returned, under the same
	// lock Stop takes. Calling SendRequest on a conn that was read before the
	// fetch is a nil interface dereference on a bare goroutine, which takes the
	// whole agent down -- and the server chooses when to disconnect, so it can
	// do it on every reconnect until the agent stays down.
	conn := i.conn
	if conn == nil {
		i.logger.Debugf("Not sending external IP addresses: the connection went away during the lookup.")
		return
	}

	if ips.IPv6 == i.current.IPv6 && ips.IPv4 == i.current.IPv4 {
		i.logger.Debugf("Clients external IP addresses did not change.")
		return
	}
	i.logger.Debugf("Client external IP addresses changed: '%s','%s'.", ips.IPv4, ips.IPv6)

	data, err := json.Marshal(ips)
	if err != nil {
		i.logger.Errorf("Failed json marshaling external IP address update: %s", err)
		return
	}

	i.logger.Debugf("Sending external IP addresses update.")
	_, _, err = conn.SendRequest(comm.RequestTypeIPAddresses, false, data)
	if err != nil {
		i.logger.Errorf("failed updating IP addresses: %s", err)
		return
	}
	i.current = *ips
}

func (i *Fetcher) refreshLoop(ctx context.Context) {
	for {
		i.sendIPAddresses(ctx)

		select {
		case <-ctx.Done():
			i.logger.Debugf("ip addresses refreshLoop finished")
			return
		case <-time.After(i.loopWait * time.Minute):
		case <-i.refreshChan:
		}
	}
}

func (i *Fetcher) SetConn(c ssh.Conn) {
	if i.IPAPIURL == "" {
		i.logger.Infof("Fetching external IP addresses disabled.")
		return
	}
	i.logger.Infof("Will fetch external IP addresses every %d min from '%s'", i.loopWait, i.IPAPIURL)
	i.mtx.Lock()
	defer i.mtx.Unlock()

	i.conn = c

}

func (i *Fetcher) Start(ctx context.Context) {
	if i.loopWait <= 0 {
		return
	}

	go i.refreshLoop(ctx)
}

// Stop drops the connection. It takes the write lock: the reader side holds
// the same mutex, and an unsynchronized write here was a data race as well as
// the source of the nil dereference described in sendIPAddresses.
func (i *Fetcher) Stop() {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	i.conn = nil
}
