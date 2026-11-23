package auth

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"sync"
	"time"
)

// BackoffMode selects what the per-account login backoff does.
type BackoffMode string

const (
	// BackoffLog computes the backoff and logs "would have blocked" but never
	// refuses a login. It is the default in this version.
	BackoffLog BackoffMode = "log"
	// BackoffEnforce refuses logins during a backoff with 429 + Retry-After
	// without verifying the password.
	BackoffEnforce BackoffMode = "enforce"
	// BackoffOff disables the feature.
	BackoffOff BackoffMode = "off"
)

const (
	defaultBackoffThreshold = 5
	defaultBackoffBase      = 2 * time.Second
	defaultBackoffMax       = 300 * time.Second
	defaultBackoffReset     = 15 * time.Minute
	defaultBackoffMaxKeys   = 10_000
)

// BackoffPolicy configures the per-account failure backoff. There is never a
// hard lockout: the delay grows by doubling and is capped at Max.
type BackoffPolicy struct {
	Mode BackoffMode
	// Threshold is the number of consecutive failures after which the next
	// attempt waits Base, then 2*Base, ... up to Max. Zero disables backoff.
	Threshold int
	Base, Max time.Duration
	// ResetAfter forgets an account that had no failure for this long
	// (default 15 minutes).
	ResetAfter time.Duration
	// MaxKeys bounds the in-memory state (LRU, default 10000).
	MaxKeys int
	// Now is a clock seam for tests.
	Now func() time.Time
}

// DefaultBackoffPolicy is the policy NewService starts with: log-only.
func DefaultBackoffPolicy() BackoffPolicy {
	return BackoffPolicy{Mode: BackoffLog, Threshold: defaultBackoffThreshold, Base: defaultBackoffBase, Max: defaultBackoffMax}
}

type backoffEntry struct {
	key          string
	failures     int
	last         time.Time
	blockedUntil time.Time
}

// loginBackoff tracks consecutive login failures per normalized username in
// memory. State is per process and lost on restart; it is bounded by an LRU so
// random usernames cannot grow it.
type loginBackoff struct {
	policy  BackoffPolicy
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List // front = most recently used
}

func newLoginBackoff(policy BackoffPolicy) *loginBackoff {
	if policy.Base <= 0 {
		policy.Base = defaultBackoffBase
	}
	if policy.Max <= 0 {
		policy.Max = defaultBackoffMax
	}
	if policy.ResetAfter <= 0 {
		policy.ResetAfter = defaultBackoffReset
	}
	if policy.MaxKeys <= 0 {
		policy.MaxKeys = defaultBackoffMaxKeys
	}
	if policy.Now == nil {
		policy.Now = func() time.Time { return time.Now().UTC() }
	}
	return &loginBackoff{policy: policy, entries: make(map[string]*list.Element), order: list.New()}
}

func (b *loginBackoff) enabled() bool {
	return b != nil && b.policy.Mode != BackoffOff && b.policy.Mode != "" && b.policy.Threshold > 0
}

func (b *loginBackoff) enforcing() bool { return b.enabled() && b.policy.Mode == BackoffEnforce }
func (b *loginBackoff) maxKeys() int    { return b.policy.MaxKeys }

func (b *loginBackoff) size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// check returns how long key must still wait; zero means it may proceed.
func (b *loginBackoff) check(key string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	element, ok := b.entries[key]
	if !ok {
		return 0
	}
	entry := element.Value.(*backoffEntry)
	now := b.policy.Now()
	if now.Sub(entry.last) >= b.policy.ResetAfter {
		b.remove(element)
		return 0
	}
	b.order.MoveToFront(element)
	if wait := entry.blockedUntil.Sub(now); wait > 0 {
		return wait
	}
	return 0
}

// fail records a failed attempt and starts or extends the backoff.
func (b *loginBackoff) fail(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.policy.Now()
	var entry *backoffEntry
	if element, ok := b.entries[key]; ok {
		entry = element.Value.(*backoffEntry)
		b.order.MoveToFront(element)
		if now.Sub(entry.last) >= b.policy.ResetAfter {
			entry.failures, entry.blockedUntil = 0, time.Time{}
		}
	} else {
		entry = &backoffEntry{key: key}
		b.entries[key] = b.order.PushFront(entry)
		for len(b.entries) > b.policy.MaxKeys {
			b.remove(b.order.Back())
		}
	}
	entry.failures++
	entry.last = now
	if b.policy.Threshold > 0 && entry.failures >= b.policy.Threshold {
		delay := b.policy.Base
		for step := entry.failures - b.policy.Threshold; step > 0 && delay < b.policy.Max; step-- {
			delay *= 2
		}
		if delay > b.policy.Max {
			delay = b.policy.Max
		}
		entry.blockedUntil = now.Add(delay)
	}
}

// success forgets the key.
func (b *loginBackoff) success(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if element, ok := b.entries[key]; ok {
		b.remove(element)
	}
}

func (b *loginBackoff) remove(element *list.Element) {
	delete(b.entries, element.Value.(*backoffEntry).key)
	b.order.Remove(element)
}

// accountHash is the short, log-safe identifier of a normalized username that
// is not known to exist.
var accountHash = func(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:4])
}

// accountLogName renders an existing account's normalized name for the log,
// quoting anything that could split or forge a line.
func accountLogName(normalized string) string {
	for _, c := range normalized {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == '@') {
			return strconv.Quote(normalized)
		}
	}
	return normalized
}

// waitingAccount is one key that is waiting out a backoff right now.
type waitingAccount struct {
	key       string
	failures  int
	remaining time.Duration
}

// waiting lists the keys whose backoff has not finished, longest wait first
// (ties by key). It reads the state without touching the LRU order or
// forgetting anything.
func (b *loginBackoff) waiting() []waitingAccount {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.policy.Now()
	var out []waitingAccount
	for _, element := range b.entries {
		entry := element.Value.(*backoffEntry)
		if now.Sub(entry.last) >= b.policy.ResetAfter {
			continue
		}
		if wait := entry.blockedUntil.Sub(now); wait > 0 {
			out = append(out, waitingAccount{key: entry.key, failures: entry.failures, remaining: wait})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].remaining != out[j].remaining {
			return out[i].remaining > out[j].remaining
		}
		return out[i].key < out[j].key
	})
	return out
}

// clear forgets one key and reports whether it had any state.
func (b *loginBackoff) clear(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	element, ok := b.entries[key]
	if ok {
		b.remove(element)
	}
	return ok
}

// keysForHash returns the keys whose short hash is hash (8 hex digits).
func (b *loginBackoff) keysForHash(hash string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var keys []string
	for key := range b.entries {
		if accountHash(key) == hash {
			keys = append(keys, key)
		}
	}
	return keys
}
