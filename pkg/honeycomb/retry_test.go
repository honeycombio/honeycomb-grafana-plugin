package honeycomb

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Internal test: backoffDelay is unexported, and it is the function that decides
// how long the plugin stalls a dashboard panel after a 429. Getting it wrong is
// either a hammered API or a panel that appears to hang.

func TestBackoffDelay_HonoursRetryAfter(t *testing.T) {
	retryAt := time.Now().Add(12 * time.Second)
	err := &APIError{StatusCode: http.StatusTooManyRequests, Body: "slow down", RetryAfter: &retryAt}

	got := backoffDelay(0, err)

	// Honeycomb told us when to come back, so that wins over our own schedule
	// even on the first attempt where exponential backoff would say ~1s.
	if got < 11*time.Second || got > 12*time.Second {
		t.Errorf("backoffDelay = %v, want ~12s from the Retry-After header", got)
	}
}

func TestBackoffDelay_IgnoresPastRetryAfter(t *testing.T) {
	past := time.Now().Add(-30 * time.Second)
	err := &APIError{StatusCode: http.StatusTooManyRequests, RetryAfter: &past}

	got := backoffDelay(0, err)

	// A stale Retry-After yields a negative duration. Returning it would make
	// the caller retry instantly and hammer the API, so fall through to backoff.
	if got <= 0 {
		t.Fatalf("backoffDelay = %v, want a positive delay", got)
	}
	if got > retryBaseDelay*2 {
		t.Errorf("backoffDelay = %v, want the attempt-0 backoff, not the stale header", got)
	}
}

func TestBackoffDelay_GrowsExponentially(t *testing.T) {
	err := &APIError{StatusCode: http.StatusInternalServerError}

	for attempt := 0; attempt < 4; attempt++ {
		base := time.Duration(float64(retryBaseDelay) * float64(int(1)<<attempt))
		maxWithJitter := base + time.Duration(float64(base)*retryJitterFactor)

		got := backoffDelay(attempt, err)

		if got < base {
			t.Errorf("attempt %d: delay %v < base %v", attempt, got, base)
		}
		if got > maxWithJitter {
			t.Errorf("attempt %d: delay %v exceeds base+jitter %v", attempt, got, maxWithJitter)
		}
	}
}

func TestBackoffDelay_IsCapped(t *testing.T) {
	err := &APIError{StatusCode: http.StatusInternalServerError}

	// A large attempt count would otherwise produce an absurd delay: attempt 20
	// is 2^20 seconds, about 12 days.
	if got := backoffDelay(20, err); got != retryMaxDelay {
		t.Errorf("backoffDelay(20) = %v, want the %v cap", got, retryMaxDelay)
	}
}

func TestBackoffDelay_AppliesJitter(t *testing.T) {
	err := &APIError{StatusCode: http.StatusInternalServerError}

	// Jitter exists so concurrent panels recovering from the same 429 do not all
	// retry in lockstep. If it were dropped, every call would return the same
	// value and the thundering herd would come back.
	seen := make(map[time.Duration]bool)
	for i := 0; i < 50; i++ {
		seen[backoffDelay(2, err)] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected jittered delays, got the same value %d times", 50)
	}
}

func TestBackoffDelay_NonAPIError(t *testing.T) {
	// Transport-level failures (DNS, connection refused) carry no Retry-After
	// and must still back off rather than returning zero.
	if got := backoffDelay(1, errors.New("connection refused")); got <= 0 {
		t.Errorf("backoffDelay for a plain error = %v, want a positive delay", got)
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"404", &APIError{StatusCode: http.StatusNotFound}, true},
		{"wrapped 404", fmt.Errorf("get slo: %w", &APIError{StatusCode: http.StatusNotFound}), true},
		{"403", &APIError{StatusCode: http.StatusForbidden}, false},
		{"429", &APIError{StatusCode: http.StatusTooManyRequests}, false},
		{"500", &APIError{StatusCode: http.StatusInternalServerError}, false},
		{"plain error", errors.New("nope"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Errorf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// The three classifiers must stay mutually exclusive: a status matching more
// than one would make the retry logic take two different paths for one error.
func TestErrorClassifiersAreDistinct(t *testing.T) {
	for _, status := range []int{
		http.StatusNotFound,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	} {
		err := &APIError{StatusCode: status}
		matches := 0
		for _, match := range []bool{IsNotFound(err), IsRateLimit(err), IsServerError(err)} {
			if match {
				matches++
			}
		}
		if matches > 1 {
			t.Errorf("status %d matches %d classifiers, want at most 1", status, matches)
		}
	}
}

func TestAPIError_Message(t *testing.T) {
	plain := &APIError{StatusCode: http.StatusUnauthorized, Body: `{"error":"unknown API key"}`}
	if got := plain.Error(); got == "" {
		t.Fatal("Error() returned an empty string")
	} else if want := "unknown API key"; !strings.Contains(got, want) {
		// The body carries the actionable part; a bare status code sends users
		// hunting through logs.
		t.Errorf("Error() = %q, want it to include %q", got, want)
	}

	retryAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	withRetry := &APIError{StatusCode: http.StatusTooManyRequests, Body: "rate limited", RetryAfter: &retryAt}
	if got := withRetry.Error(); !strings.Contains(got, "retry after") {
		t.Errorf("Error() = %q, want it to mention the retry time", got)
	}
}
