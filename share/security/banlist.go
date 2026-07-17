package security

import (
	"sync"
	"time"

	"github.com/proximile/proxiport/share/logger"
)

type BanList struct {
	banDuration time.Duration
	mu          sync.RWMutex
	visitors    map[string]time.Time
}

func NewBanList(banDuration time.Duration) *BanList {
	return &BanList{
		banDuration: banDuration,
		visitors:    make(map[string]time.Time),
	}
}

func (l *BanList) Add(visitorKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.visitors[visitorKey] = time.Now().Add(l.banDuration)
}

func (l *BanList) IsBanned(visitorKey string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	banExpiry, found := l.visitors[visitorKey]
	return found && banExpiry.After(time.Now())
}

// MaxBadAttemptsBanList bans visitors by their keys after N failed consecutive attempts for Z period.
type MaxBadAttemptsBanList struct {
	banDuration    time.Duration
	maxBadAttempts int
	mu             sync.RWMutex
	visitors       map[string]*visitor
	logger         *logger.Logger
}

type visitor struct {
	badAttempts int
	banTime     *time.Time
	// seenFailures fingerprints the distinct failing credentials counted toward
	// a ban for this visitor, so that a client retrying the SAME wrong
	// credential is counted once instead of racking up a self-inflicted ban.
	// Only used by AddDistinctBadAttempt. Reset on ban or success.
	seenFailures map[string]struct{}
}

func NewMaxBadAttemptsBanList(maxBadAttempts int, banDuration time.Duration, logger *logger.Logger) *MaxBadAttemptsBanList {
	return &MaxBadAttemptsBanList{
		banDuration:    banDuration,
		maxBadAttempts: maxBadAttempts,
		visitors:       make(map[string]*visitor),
		logger:         logger,
	}
}

// AddBadAttempt registers a bad attempt of a visitor.
func (l *MaxBadAttemptsBanList) AddBadAttempt(visitorKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	v, found := l.visitors[visitorKey]
	if !found {
		v = &visitor{}
		l.visitors[visitorKey] = v
	}

	v.badAttempts++

	if v.badAttempts == l.maxBadAttempts {
		t := time.Now().Add(l.banDuration)
		if l.logger != nil {
			l.logger.Infof("Maximum of %d login attempts reached. Visitor (%s) banned. Ban expiry: %s", v.badAttempts, visitorKey, t.Format(time.RFC3339))
		}
		v.banTime = &t
		v.badAttempts = 0
	}
}

// AddDistinctBadAttempt is like AddBadAttempt but only counts a failure whose
// dedupKey has not already been seen for this visitor since its last success or
// ban. This distinguishes a single misconfigured client retrying one wrong
// credential (connection churn — counted once, never self-bans) from an
// attacker trying many distinct credentials (each counted — banned as before).
//
// dedupKey must be a non-reversible fingerprint of the attempted credential; it
// is held only in memory and never logged or persisted. maxBadAttempts is thus
// interpreted as "distinct failing credentials", not raw attempts.
func (l *MaxBadAttemptsBanList) AddDistinctBadAttempt(visitorKey, dedupKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	v, found := l.visitors[visitorKey]
	if !found {
		v = &visitor{}
		l.visitors[visitorKey] = v
	}
	if v.seenFailures == nil {
		v.seenFailures = make(map[string]struct{})
	}
	if _, dup := v.seenFailures[dedupKey]; dup {
		// Same failing credential retried — not brute force. Don't count it, so
		// a legitimate client's own retry loop can't ban its own IP.
		return
	}
	v.seenFailures[dedupKey] = struct{}{}
	v.badAttempts++

	if v.badAttempts == l.maxBadAttempts {
		t := time.Now().Add(l.banDuration)
		if l.logger != nil {
			l.logger.Infof("Maximum of %d distinct failed credentials reached. Visitor (%s) banned. Ban expiry: %s", v.badAttempts, visitorKey, t.Format(time.RFC3339))
		}
		v.banTime = &t
		v.badAttempts = 0
		v.seenFailures = nil
	}
}

// AddSuccessAttempt registers a successful attempt of a visitor.
func (l *MaxBadAttemptsBanList) AddSuccessAttempt(visitorKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	v, found := l.visitors[visitorKey]
	if found {
		v.badAttempts = 0
		v.banTime = nil
		v.seenFailures = nil
	}
}

// IsBanned checks whether a given visitor is banned or not.
func (l *MaxBadAttemptsBanList) IsBanned(visitorKey string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	v, found := l.visitors[visitorKey]
	return found && v.banTime != nil && v.banTime.After(time.Now())
}
