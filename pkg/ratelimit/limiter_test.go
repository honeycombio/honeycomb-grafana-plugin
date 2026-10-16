package ratelimit_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/ratelimit"
)

func TestLimiter_AllowsBurstImmediately(t *testing.T) {
	lim := ratelimit.New(0)

	// The burst is 3, so the first 3 calls should succeed without waiting.
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := lim.Wait(context.Background()); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	// Allow generous slack for slow CI environments.
	if elapsed > 500*time.Millisecond {
		t.Errorf("burst calls took too long: %s", elapsed)
	}
}

func TestLimiter_RespectsContextCancellation(t *testing.T) {
	lim := ratelimit.New(0)

	// Drain the burst.
	for i := 0; i < 3; i++ {
		lim.Reserve()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := lim.Wait(ctx)
	if err == nil {
		t.Error("expected error when context times out while waiting for token")
	}
}

func TestLimiter_ReturnsTokensAvailability(t *testing.T) {
	lim := ratelimit.New(0)

	tokens := lim.Tokens()
	if tokens <= 0 {
		t.Errorf("expected positive tokens on fresh limiter, got %f", tokens)
	}
}

func TestLimiter_Reserve_NonBlocking(t *testing.T) {
	lim := ratelimit.New(0)

	// First few calls should be non-blocking (burst).
	got := lim.Reserve()
	if !got {
		t.Error("expected Reserve() to succeed on fresh limiter")
	}
}

func TestLimiter_MaxWaitTimeout(t *testing.T) {
	lim := ratelimit.New(0)

	// Drain burst.
	for i := 0; i < 3; i++ {
		lim.Reserve()
	}

	// With 8 tokens/min rate and 0 tokens available, Wait should timeout
	// rather than blocking indefinitely. We use a very short context to
	// avoid making the test slow.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := lim.Wait(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("Wait should have failed quickly, took %s", elapsed)
	}
}

func TestLimiter_DefaultRate(t *testing.T) {
	for _, in := range []int{0, -5} {
		if got := ratelimit.New(in).TokensPerMinute(); got != ratelimit.DefaultTokensPerMinute {
			t.Errorf("New(%d).TokensPerMinute() = %d, want %d", in, got, ratelimit.DefaultTokensPerMinute)
		}
	}
}

func TestLimiter_CustomRate(t *testing.T) {
	if got := ratelimit.New(5).TokensPerMinute(); got != 5 {
		t.Errorf("TokensPerMinute() = %d, want 5", got)
	}
}

func TestLimiter_BurstCappedAtRate(t *testing.T) {
	lim := ratelimit.New(1)

	if !lim.Reserve() {
		t.Fatal("expected first Reserve() to succeed")
	}
	if lim.Reserve() {
		t.Error("expected burst of 1 when rate is 1/min, but second Reserve() succeeded")
	}
}

func TestLimiter_MaxWaitExceeded_ExplainsSetting(t *testing.T) {
	// At 1/min the next token is 60 s away, beyond maxWait (30 s), so Wait
	// should fail immediately with the explanatory error.
	lim := ratelimit.New(1)
	lim.Reserve()

	start := time.Now()
	err := lim.Wait(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when next token is beyond maxWait")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Wait should fail fast, took %s", elapsed)
	}
	if !strings.Contains(err.Error(), "1 req/min (Rate Limit setting)") {
		t.Errorf("error should name the configured rate and setting, got: %v", err)
	}
}

func TestLimiter_CallerDeadline_ReportsCancellation(t *testing.T) {
	// A caller deadline shorter than maxWait is the caller's limit, not ours.
	lim := ratelimit.New(1)
	lim.Reserve()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := lim.Wait(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "Rate Limit setting") {
		t.Errorf("caller deadline should not be reported as a rate-limit setting problem, got: %v", err)
	}
}
