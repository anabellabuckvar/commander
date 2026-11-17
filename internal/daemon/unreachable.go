package daemon

import (
	"sync"
	"time"
)

// unreachableDelays space the retries of a message whose target role has no
// running process (chat.ErrNotRunning). After the last one the message is
// given up: a dead run is liveness's to settle, not a reason to retry its
// mail every few seconds forever.
var unreachableDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// unreachable tracks, per message key, the failed tries to reach a role with
// no running process.
type unreachable struct {
	mu sync.Mutex
	m  map[string]*unreachEntry
}

type unreachEntry struct {
	tries int
	next  time.Time
}

func newUnreachable() *unreachable { return &unreachable{m: map[string]*unreachEntry{}} }

// due reports whether key may be tried at now (a nil tracker always may).
func (u *unreachable) due(key string, now time.Time) bool {
	if u == nil {
		return true
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	e, ok := u.m[key]
	return !ok || !now.Before(e.next)
}

// failed records a try that found no running process: first is the first
// failure for key (log it), giveUp is the one after the last delay.
func (u *unreachable) failed(key string, now time.Time) (first, giveUp bool) {
	if u == nil {
		return true, false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	e, ok := u.m[key]
	if !ok {
		e = &unreachEntry{}
		u.m[key] = e
	}
	if e.tries >= len(unreachableDelays) {
		delete(u.m, key)
		return false, true
	}
	e.next = now.Add(unreachableDelays[e.tries])
	e.tries++
	return e.tries == 1, false
}

// clear forgets key (delivered, or given up).
func (u *unreachable) clear(key string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	delete(u.m, key)
	u.mu.Unlock()
}
