package auth

import (
	"context"
	"log"
	"runtime"
	"time"

	"regixtry/internal/ports"
)

// defaultLoginMaxWait is how long a password login waits for a free bcrypt
// slot before it is refused.
const defaultLoginMaxWait = 250 * time.Millisecond

// loginRetryAfter is the Retry-After hint for a refusal caused by the
// concurrency limiter (a slot frees up within about one comparison).
const loginRetryAfter = time.Second

// LoginPolicy configures the login hardening of a Service. The zero value is
// the safe default: automatic bcrypt concurrency, 250 ms max wait.
type LoginPolicy struct {
	// MaxConcurrency caps simultaneous bcrypt comparisons; 0 means
	// max(2, NumCPU/2).
	MaxConcurrency int
	// MaxWait is how long a login waits for a slot; 0 means 250 ms.
	MaxWait time.Duration
	// Logger receives backoff and limiter notices; nil uses the default logger.
	Logger *log.Logger
	// Compare replaces the bcrypt comparison. It is a test seam: nil keeps
	// bcrypt.
	Compare func(hash, password []byte) error
}

// autoLoginConcurrency is max(2, cpus/2): enough parallelism for legitimate
// logins while leaving CPU to the registry's other endpoints.
func autoLoginConcurrency(cpus int) int {
	if n := cpus / 2; n > 2 {
		return n
	}
	return 2
}

// loginLimiter is a counting semaphore around the bcrypt comparison.
type loginLimiter struct {
	slots   chan struct{}
	maxWait time.Duration
}

func newLoginLimiter(maxConcurrency int, maxWait time.Duration) *loginLimiter {
	if maxConcurrency <= 0 {
		maxConcurrency = autoLoginConcurrency(runtime.NumCPU())
	}
	if maxWait <= 0 {
		maxWait = defaultLoginMaxWait
	}
	return &loginLimiter{slots: make(chan struct{}, maxConcurrency), maxWait: maxWait}
}

func (l *loginLimiter) capacity() int { return cap(l.slots) }

// acquire takes a slot, waiting at most maxWait. It returns a
// *ports.LoginThrottledError when none frees up in time, or the context's
// error when the caller gives up first.
func (l *loginLimiter) acquire(ctx context.Context) error {
	select {
	case l.slots <- struct{}{}:
		return nil
	default:
	}
	timer := time.NewTimer(l.maxWait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-timer.C:
		return &ports.LoginThrottledError{RetryAfter: loginRetryAfter, Reason: "throttled: bcrypt concurrency limit"}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *loginLimiter) release() { <-l.slots }

// ConfigureLogin applies the login policy. Call it once at wiring time,
// before the service handles requests.
func (s *Service) ConfigureLogin(policy LoginPolicy) {
	s.limiter = newLoginLimiter(policy.MaxConcurrency, policy.MaxWait)
	if policy.Compare != nil {
		s.comparePassword = policy.Compare
	}
	if policy.Logger != nil {
		s.logger = policy.Logger
	}
}

// compareLimited runs the bcrypt comparison inside the concurrency limiter.
// A refused acquisition never reaches bcrypt.
func (s *Service) compareLimited(ctx context.Context, hash, password []byte) (bool, error) {
	if err := s.limiter.acquire(ctx); err != nil {
		return false, err
	}
	defer s.limiter.release()
	return s.comparePassword(hash, password) == nil, nil
}
