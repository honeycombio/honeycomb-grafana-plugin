# ADR-003: Rate Limiting Strategy

**Status:** Accepted  
**Date:** 2025-01-15

## Context

Honeycomb's Create Query Result endpoint is limited to **10 requests per minute**. The Query Data 429 responses **do not include a `Retry-After` header** (unlike other Honeycomb endpoints). This means callers cannot rely on the header and must implement their own safe fallback.

## Decision

### Token bucket rate limiter

Wrap every Create Query Result call (before HTTP is issued) with a `golang.org/x/time/rate` token bucket configured as:

- **Rate:** 8 tokens per 60 seconds (leaving a 20% headroom below the hard 10/min cap)
- **Burst:** 3 tokens (allows a short burst of back-to-back panel loads without queuing)
- **Max wait:** 30 seconds (if no token is available within 30 s, the query fails with a descriptive error)

The 20% headroom accounts for:
- Clock skew between the plugin and Honeycomb's rate limit window
- Other API clients sharing the same API key (e.g., CLI, other integrations)

Rate: 8/60 ≈ 0.133 tokens/second.

**Amendment:** the rate is configurable per data source via `rateLimitPerMinute` (default 8). The burst is `min(3, rate)` so a low setting still holds during a cold start.

### 429 handling with fallback backoff

Even with the token bucket, a 429 can occur (other clients sharing the key, or the Honeycomb window resetting at a different boundary). On a 429 from Create Query Result:

1. Check `Retry-After` header; if present, sleep until that time
2. If absent (expected per Honeycomb docs for Query Data), use exponential backoff:
   - Attempt 1: wait 10 s
   - Attempt 2: wait 20 s + jitter (0–5 s)
   - Attempt 3: wait 40 s + jitter (0–10 s)
   - Maximum 3 retries, then return error to Grafana
3. Jitter is `rand.Float64() * attempt_seconds * 0.25`

### Rate limit header monitoring

On every Honeycomb response, parse `RateLimit` and `RateLimit-Policy` headers. Log the remaining capacity at DEBUG level. If remaining < 3, log at WARN to alert operators before hitting 429s.

### General endpoint rate limiting

Create Query and Get Query Result are not subject to the 10/min limit and do not need the token bucket. They are protected only by HTTP retries with exponential backoff on 429:
- Max 3 retries
- Base delay 1 s, factor 2×, cap 30 s, 25% jitter

## Consequences

**Positive:**
- Steady-state dashboards will almost never see a 429 because L3 cache serves most requests
- Token bucket prevents worst-case burst scenarios
- Fallback backoff handles multi-client scenarios safely

**Negative:**
- At 8 tokens/min, a cold start with 9 panels will have the 9th panel wait ~7.5 s for a token. This is acceptable and expected behavior, made visible via query status messages.
- The fallback backoff (10 s, 20 s, 40 s) means a 429 causes up to ~70 s of delay. Log a clear error so operators know to check their API key usage.
