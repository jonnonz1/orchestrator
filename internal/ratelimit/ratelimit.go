// Package ratelimit provides a semaphore for concurrent VM/task caps and
// a token-bucket rate limiter for task creation.
package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// Limiter enforces both concurrency and rate limits.
//
//   - Concurrency: a semaphore with ORCHESTRATOR_MAX_CONCURRENT_VMS capacity.
//     Acquire blocks until a slot opens or ctx expires.
//   - Rate: a token bucket refilled at ORCHESTRATOR_TASK_RATE_LIMIT per minute.
//     If the bucket is empty, TryAcquire returns false (non-blocking).
//
// Both are optional — 0 disables the respective limit.
type Limiter struct {
	sem        chan struct{}
	maxConc    int
	ratePerMin int

	mu     sync.Mutex
	tokens int
	lastAt time.Time
}

// FromEnv reads ORCHESTRATOR_MAX_CONCURRENT_VMS and ORCHESTRATOR_TASK_RATE_LIMIT from env.
// Returns nil if both are 0 (no limits).
func FromEnv() *Limiter {
	maxConc, _ := strconv.Atoi(os.Getenv("ORCHESTRATOR_MAX_CONCURRENT_VMS"))
	ratePerMin, _ := strconv.Atoi(os.Getenv("ORCHESTRATOR_TASK_RATE_LIMIT"))
	return New(maxConc, ratePerMin)
}

// New creates a Limiter. Zero means unlimited for that dimension.
func New(maxConcurrent, ratePerMinute int) *Limiter {
	l := &Limiter{
		maxConc:    maxConcurrent,
		ratePerMin: ratePerMinute,
		tokens:     ratePerMinute,
		lastAt:     time.Now(),
	}
	if maxConcurrent > 0 {
		l.sem = make(chan struct{}, maxConcurrent)
	}
	return l
}

// Acquire blocks until a concurrency slot is available or ctx expires.
// Returns a release function that must be called when the task/VM finishes.
func (l *Limiter) Acquire(ctx context.Context) (release func(), err error) {
	if l == nil || l.sem == nil {
		return func() {}, nil
	}
	select {
	case l.sem <- struct{}{}:
		return func() { <-l.sem }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("concurrency limit (%d) reached: %w", l.maxConc, ctx.Err())
	}
}

// TryRate checks the rate limit (non-blocking). Returns true if a token was
// consumed. Returns false (+ 429-style error) if the bucket is empty.
func (l *Limiter) TryRate() error {
	if l == nil || l.ratePerMin <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	if l.tokens <= 0 {
		return fmt.Errorf("rate limit exceeded (%d tasks/min)", l.ratePerMin)
	}
	l.tokens--
	return nil
}

func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastAt)
	add := int(elapsed.Minutes() * float64(l.ratePerMin))
	if add > 0 {
		l.tokens += add
		if l.tokens > l.ratePerMin {
			l.tokens = l.ratePerMin
		}
		l.lastAt = now
	}
}

// Active returns the current number of in-use concurrency slots.
func (l *Limiter) Active() int {
	if l == nil || l.sem == nil {
		return 0
	}
	return len(l.sem)
}

// MaxConcurrent returns the configured limit.
func (l *Limiter) MaxConcurrent() int {
	if l == nil {
		return 0
	}
	return l.maxConc
}

// HTTPMiddleware returns a middleware that checks the rate limit on POST
// requests to task-creation endpoints. Returns 429 when exceeded.
func (l *Limiter) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil || l.ratePerMin <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && (r.URL.Path == "/api/v1/tasks" || r.URL.Path == "/api/v1/vms") {
				if err := l.TryRate(); err != nil {
					w.Header().Set("Retry-After", "60")
					http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusTooManyRequests)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
