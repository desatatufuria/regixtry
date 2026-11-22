package ports

import (
	"time"
)

// LoginThrottledError is returned by an AuthService when a password login is
// refused before any password is verified: the per-account backoff is active or
// the bcrypt concurrency limiter has no free slot. Adapters map it to HTTP 429
// with a Retry-After header. It is deliberately not an invalid-credentials
// error: callers must not fall through to another credential check.
type LoginThrottledError struct {
	// RetryAfter is how long the caller should wait; adapters round it up to
	// whole seconds.
	RetryAfter time.Duration
	// Reason is the server-side cause, for logs only: it is never sent to the
	// client, whose response is the same for every cause.
	Reason string
}

func (e *LoginThrottledError) Error() string {
	return "too many login attempts; retry later"
}

// RetryAfterSeconds is RetryAfter rounded up to whole seconds, at least 1.
func (e *LoginThrottledError) RetryAfterSeconds() int {
	seconds := int((e.RetryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// IsCredentialTokenShaped reports whether secret has the exact shape of a
// credential token (robot or personal access token): 64 lowercase hex
// characters, the encoding of 32 random bytes. The shape is public; it only
// selects the cheaper exchange path and keeps a wrong token-shaped secret out
// of any per-account failure accounting.
func IsCredentialTokenShaped(secret string) bool {
	if len(secret) != 64 {
		return false
	}
	for i := 0; i < len(secret); i++ {
		c := secret[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
