package chserver

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/proximile/proxiport/server/api"
)

// wsTicketTTL bounds how long a freshly-issued WebSocket ticket is usable.
// Tickets are single-use, so this only needs to cover the round-trip between
// asking for a ticket and opening the socket.
const wsTicketTTL = 30 * time.Second

// WebSocketTicketQueryParam carries a one-time ticket on the WebSocket upgrade
// URL. Browsers cannot set request headers on a WebSocket handshake, so some
// value has to ride the URL; a single-use, short-lived ticket is safe to expose
// there (and safe to appear in an access log) in a way the long-lived bearer JWT
// never was.
const WebSocketTicketQueryParam = "ticket"

type wsTicketEntry struct {
	username  string
	expiresAt time.Time
}

// wsTicketStore hands out and redeems one-time WebSocket auth tickets. A ticket
// is bound to the user who requested it (over a normal bearer-authenticated API
// call) and is consumed on first redemption.
type wsTicketStore struct {
	mu      sync.Mutex
	tickets map[string]wsTicketEntry
}

func newWSTicketStore() *wsTicketStore {
	return &wsTicketStore{tickets: make(map[string]wsTicketEntry)}
}

func (s *wsTicketStore) issue(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.tickets { // opportunistic sweep of expired tickets
		if now.After(v.expiresAt) {
			delete(s.tickets, k)
		}
	}
	s.tickets[ticket] = wsTicketEntry{username: username, expiresAt: now.Add(wsTicketTTL)}
	return ticket, nil
}

// redeem consumes a ticket (whether or not it was still valid) and returns the
// bound username when the ticket existed and had not expired.
func (s *wsTicketStore) redeem(ticket string) (username string, ok bool) {
	if ticket == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, found := s.tickets[ticket]
	if !found {
		return "", false
	}
	delete(s.tickets, ticket) // single-use
	if time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.username, true
}

// handleGetWSTicket issues a one-time ticket for the authenticated user. It sits
// behind the bearer-auth (and scope) middleware, so a 2FA-pending token — scoped
// only to the verify-2FA route — cannot obtain one.
func (al *APIListener) handleGetWSTicket(w http.ResponseWriter, req *http.Request) {
	user, err := al.getUserModelForAuth(req.Context())
	if err != nil {
		al.jsonError(w, err)
		return
	}
	ticket, err := al.wsTickets.issue(user.Username)
	if err != nil {
		al.jsonErrorResponse(w, http.StatusInternalServerError, err)
		return
	}
	al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(map[string]string{"ticket": ticket}))
}
