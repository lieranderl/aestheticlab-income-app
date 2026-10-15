package web

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Login throttling per source (technical-solution §6), on top of the
// per-account lock: after sourceLimit failed logins from one address within
// sourceWindow, further logins from it are refused before any password
// check. Counts live in this instance's memory, which is enough for a small
// Cloud Run service; a restart forgets them.
const (
	sourceLimit   = 10
	sourceWindow  = 15 * time.Minute
	maxSources    = 10_000
	maxHashing    = 4               // bcrypt compares running at once
	hashQueueWait = 5 * time.Second // longest wait for a free compare
)

type sourceCount struct {
	attempts int // admitted logins that did not succeed
	since    time.Time
}

type loginThrottle struct {
	now     func() time.Time
	mu      sync.Mutex
	sources map[string]*sourceCount
	hashing chan struct{}
	reauth  map[string]bool // people whose current password is being checked
}

func newLoginThrottle(now func() time.Time) *loginThrottle {
	return &loginThrottle{now: now, sources: map[string]*sourceCount{}, hashing: make(chan struct{}, maxHashing), reauth: map[string]bool{}}
}

// admit lets a login from source go ahead unless the source has used up
// its attempts, and counts it at once, under the lock, so a burst of
// parallel requests cannot all slip past the limit before any of them
// fails. A login that succeeds forgets the count (succeeded); one that was
// never checked gives its attempt back (release).
func (t *loginThrottle) admit(source string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if c := t.count(source, now); c != nil {
		if c.attempts >= sourceLimit {
			return false
		}
		c.attempts++
		return true
	}
	if len(t.sources) >= maxSources {
		t.prune(now)
	}
	if len(t.sources) >= maxSources {
		source = floodKey
	}
	t.sources[source] = &sourceCount{attempts: 1, since: now}
	return true
}

// release gives back an attempt that never reached a password check.
func (t *loginThrottle) release(source string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.count(source, t.now()); c != nil && c.attempts > 0 {
		c.attempts--
	}
}

// floodKey counts every source that arrives while maxSources are tracked.
const floodKey = "*"

// count is source's current count, or the shared flood count for an
// untracked source while the table is full; nil when there is none.
func (t *loginThrottle) count(source string, now time.Time) *sourceCount {
	c := t.sources[source]
	if c == nil && len(t.sources) >= maxSources {
		c = t.sources[floodKey]
	}
	if c == nil || now.Sub(c.since) >= sourceWindow {
		return nil
	}
	return c
}

// succeeded forgets the source's attempts.
func (t *loginThrottle) succeeded(source string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sources, source)
}

func (t *loginThrottle) prune(now time.Time) {
	for k, c := range t.sources {
		if now.Sub(c.since) >= sourceWindow {
			delete(t.sources, k)
		}
	}
}

// beginReauth claims the one current-password check a person may have
// running, so parallel requests cannot each guess before any of them
// records a failure; false means one is already running. endReauth frees it.
func (t *loginThrottle) beginReauth(uid string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reauth[uid] {
		return false
	}
	t.reauth[uid] = true
	return true
}

func (t *loginThrottle) endReauth(uid string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.reauth, uid)
}

// hash runs a bcrypt compare once one of maxHashing slots is free, so a
// flood of logins cannot use all the CPU; false means none freed up in time.
func (t *loginThrottle) hash(r *http.Request, compare func()) bool {
	timer := time.NewTimer(hashQueueWait)
	defer timer.Stop()
	select {
	case t.hashing <- struct{}{}:
	case <-timer.C:
		return false
	case <-r.Context().Done():
		return false
	}
	defer func() { <-t.hashing }()
	compare()
	return true
}

// loginSource is where a login comes from: the last X-Forwarded-For entry,
// which Cloud Run's front end appends (earlier entries are the client's
// own and can be anything), else the connection's address. IPv6 addresses
// count per /64, since one client usually owns a whole /64.
func loginSource(r *http.Request) string {
	addr := ""
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		last := xff[len(xff)-1]
		if i := strings.LastIndex(last, ","); i >= 0 {
			last = last[i+1:]
		}
		addr = strings.TrimSpace(last)
	}
	if addr == "" {
		addr = r.RemoteAddr
		if host, _, err := net.SplitHostPort(addr); err == nil {
			addr = host
		}
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return addr
	}
	if ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return ip.String()
}
