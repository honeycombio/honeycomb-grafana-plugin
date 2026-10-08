// Package ratelimit provides a token-bucket rate limiter protecting the
// Honeycomb Create Query Result endpoint (hard cap: 10 requests/minute).
//
// See ADR-003 for the full rationale. Key decisions:
//   - Rate: 8/60 tokens per second by default (20% headroom below Honeycomb's
//     hard cap), overridable per datasource via rateLimitPerMinute
//   - Burst: 3, capped at the per-minute rate (allows a short initial burst
//     before queuing)
//   - Max wait: 30 s before returning an error to Grafana
//
// The limiter is per-datasource-instance (one per API key), created in
// NewDatasource and held in the Datasource struct.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultTokensPerMinute is our conservative limit (20% below Honeycomb's 10/min cap).
	DefaultTokensPerMinute = 8
	// burstSize allows a short burst of back-to-back submissions at startup.
	burstSize = 3
	// maxWait is the longest we will block waiting for a token.
	maxWait = 30 * time.Second
)

// Limiter wraps golang.org/x/time/rate.Limiter with Honeycomb-specific settings.
type Limiter struct {
	rl              *rate.Limiter
	tokensPerMinute int
}

// New returns a Limiter for the Create Query Result endpoint that allows
// tokensPerMinute requests per minute. Values <= 0 use DefaultTokensPerMinute.
func New(tokensPerMinute int) *Limiter {
	if tokensPerMinute <= 0 {
		tokensPerMinute = DefaultTokensPerMinute
	}
	// A burst larger than the rate would let a cold start exceed the
	// configured per-minute budget.
	burst := min(burstSize, tokensPerMinute)
	r := rate.Every(time.Minute / time.Duration(tokensPerMinute))
	return &Limiter{rl: rate.NewLimiter(r, burst), tokensPerMinute: tokensPerMinute}
}

// TokensPerMinute returns the configured steady-state rate.
func (l *Limiter) TokensPerMinute() int {
	return l.tokensPerMinute
}

// Wait blocks until a token is available or ctx is cancelled.
// Returns an error if ctx expires before a token becomes available, or if
// the wait would exceed maxWait.
//
// Callers should call Wait immediately before invoking
// honeycomb.Client.CreateQueryResult.
func (l *Limiter) Wait(ctx context.Context) error {
	// Create a child context with a hard maxWait deadline so we do not block
	// indefinitely even if ctx has no deadline.
	waitCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	if err := l.rl.Wait(waitCtx); err != nil {
		if waitCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf(
				"timed out waiting for Honeycomb rate limit token after %s "+
					"(this datasource limits Create Query Result to %d req/min; "+
					"too many concurrent panels may be competing for capacity)",
				maxWait, l.tokensPerMinute)
		}
		return fmt.Errorf("rate limiter context cancelled: %w", err)
	}
	return nil
}

// Reserve returns an immediately-available token if possible without blocking.
// Returns false if no token is available right now (non-blocking probe).
func (l *Limiter) Reserve() bool {
	return l.rl.Allow()
}

// Tokens returns the current number of available tokens (may be fractional).
func (l *Limiter) Tokens() float64 {
	return l.rl.Tokens()
}
