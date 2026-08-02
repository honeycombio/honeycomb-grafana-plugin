package honeycomb

import (
	"bytes"
	"context"
	stderrors "errors"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

const (
	defaultAPIURL      = "https://api.honeycomb.io"
	defaultHTTPTimeout = 30 * time.Second

	pluginVersion = "0.1.3"

	// Polling config for Get Query Result.
	pollInitialInterval = 200 * time.Millisecond
	pollMaxInterval     = 1 * time.Second
	pollMaxDuration     = 30 * time.Second

	// Retry config for non-rate-limited endpoints (Create Query, Get Query Result, metadata).
	maxRetries        = 3
	retryBaseDelay    = 1 * time.Second
	retryMaxDelay     = 30 * time.Second
	retryJitterFactor = 0.25
)

// Client is a typed HTTP client for the Honeycomb API.
// It is safe for concurrent use; one instance should be shared across all
// queries from the same datasource instance.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	logger     log.Logger
}

// Config holds the settings needed to create a Client.
type Config struct {
	// APIURL is the Honeycomb API base URL.
	// Defaults to https://api.honeycomb.io.
	// Use https://api.eu1.honeycomb.io for EU accounts.
	APIURL string
	// APIKey is the Honeycomb Configuration API key. Required.
	// Must be stored in secureJsonData and never passed via jsonData.
	APIKey string
}

// New returns a configured Honeycomb API client.
func New(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(cfg.APIURL, "/")
	if baseURL == "" {
		baseURL = defaultAPIURL
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("invalid honeycomb API URL %q: %w", baseURL, err)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("honeycomb API key is required")
	}
	return &Client{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		logger:     log.DefaultLogger,
	}, nil
}

// ---------------------------------------------------------------------------
// Public API methods
// ---------------------------------------------------------------------------

// CreateQuery sends the query specification to Honeycomb and returns the
// assigned query ID. The returned ID is stable for a given spec; callers
// should cache it (see pkg/cache).
func (c *Client) CreateQuery(ctx context.Context, dataset string, q Query) (string, error) {
	path := fmt.Sprintf("/1/queries/%s", url.PathEscape(dataset))
	var resp QueryResponse
	if err := c.doWithRetry(ctx, http.MethodPost, path, q, &resp); err != nil {
		return "", fmt.Errorf("create query for dataset %q: %w", dataset, err)
	}
	return resp.ID, nil
}

// CreateQueryResult submits a query for execution and returns the query
// result ID and the initial links (which include the Honeycomb UI URL).
//
// IMPORTANT: This call is limited to 10 requests/minute on Honeycomb's side.
// Callers MUST acquire a rate-limit token before calling this method.
// No automatic retry is applied here; the token bucket and caller-level
// backoff handle pacing.
func (c *Client) CreateQueryResult(ctx context.Context, dataset string, req QueryResultRequest) (*QueryResultCreateResponse, error) {
	path := fmt.Sprintf("/1/query_results/%s", url.PathEscape(dataset))
	var resp QueryResultCreateResponse
	if err := c.do(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, fmt.Errorf("create query result for dataset %q: %w", dataset, err)
	}
	return &resp, nil
}

// GetQueryResult fetches the current state of a query result. It polls until
// the result is marked complete, or until ctx is cancelled, or until
// pollMaxDuration elapses.
func (c *Client) GetQueryResult(ctx context.Context, dataset, queryResultID string) (*QueryResultResponse, error) {
	path := fmt.Sprintf("/1/query_results/%s/%s", url.PathEscape(dataset), url.PathEscape(queryResultID))

	deadline := time.Now().Add(pollMaxDuration)
	interval := pollInitialInterval

	for {
		var resp QueryResultResponse
		if err := c.doWithRetry(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, fmt.Errorf("get query result %q (dataset=%q): %w", queryResultID, dataset, err)
		}
		if resp.Complete {
			return &resp, nil
		}

		remaining := time.Until(deadline)
		if remaining <= interval {
			return nil, fmt.Errorf("timed out waiting for query result %q (dataset=%q) after %s",
				queryResultID, dataset, pollMaxDuration)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		// Exponential backoff capped at pollMaxInterval.
		interval = time.Duration(math.Min(float64(interval*2), float64(pollMaxInterval)))
	}
}

// HealthCheck performs a lightweight API call to verify connectivity and
// authentication. Returns a descriptive error on failure.
func (c *Client) HealthCheck(ctx context.Context) error {
	var raw json.RawMessage
	if err := c.doWithRetry(ctx, http.MethodGet, "/1/datasets", nil, &raw); err != nil {
		return fmt.Errorf("honeycomb health check failed: %w", err)
	}
	return nil
}

// ListDatasets returns the dataset slugs visible to the API key.
func (c *Client) ListDatasets(ctx context.Context) ([]DatasetMeta, error) {
	var datasets []DatasetMeta
	if err := c.doWithRetry(ctx, http.MethodGet, "/1/datasets", nil, &datasets); err != nil {
		return nil, fmt.Errorf("list datasets: %w", err)
	}
	return datasets, nil
}

// ListColumns returns the columns for a given dataset.
func (c *Client) ListColumns(ctx context.Context, dataset string) ([]ColumnMeta, error) {
	path := fmt.Sprintf("/1/columns/%s", url.PathEscape(dataset))
	var cols []ColumnMeta
	if err := c.doWithRetry(ctx, http.MethodGet, path, nil, &cols); err != nil {
		return nil, fmt.Errorf("list columns for dataset %q: %w", dataset, err)
	}
	return cols, nil
}

// ListSLOs returns the SLOs for a given dataset (or "__all__" for environment-wide).
//
// Spec: https://docs.honeycomb.io/api/slos/get-all-slos.md
//
// Note: detailed metrics (compliance, burn_rate, etc.) are NOT returned by this
// endpoint. Call GetSLO with detailed=true for a single SLO's metrics.
func (c *Client) ListSLOs(ctx context.Context, dataset string) ([]SLO, error) {
	path := fmt.Sprintf("/1/slos/%s", url.PathEscape(dataset))
	var slos []SLO
	if err := c.doWithRetry(ctx, http.MethodGet, path, nil, &slos); err != nil {
		return nil, fmt.Errorf("list SLOs for dataset %q: %w", dataset, err)
	}
	return slos, nil
}

// GetSLO fetches a single SLO. When detailed=true, the response includes
// compliance, budget_remaining, status, and burn_rate (last-4-hours).
//
// Spec: https://docs.honeycomb.io/api/slos/get-an-slo.md
func (c *Client) GetSLO(ctx context.Context, dataset, sloID string, detailed bool) (*SLO, error) {
	path := fmt.Sprintf("/1/slos/%s/%s", url.PathEscape(dataset), url.PathEscape(sloID))
	if detailed {
		path += "?detailed=true"
	}
	var slo SLO
	if err := c.doWithRetry(ctx, http.MethodGet, path, nil, &slo); err != nil {
		return nil, fmt.Errorf("get SLO %q (dataset=%q): %w", sloID, dataset, err)
	}
	return &slo, nil
}

// DatasetMeta is metadata about a Honeycomb dataset.
type DatasetMeta struct {
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// ColumnMeta is metadata about a column within a dataset.
type ColumnMeta struct {
	ID          string    `json:"id"`
	KeyName     string    `json:"key_name"`
	Type        string    `json:"type"` // string, float, integer, boolean
	Hidden      bool      `json:"hidden"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	LastWritten time.Time `json:"last_written"`
}

// ---------------------------------------------------------------------------
// HTTP internals
// ---------------------------------------------------------------------------

// do performs a single HTTP request without automatic retries.
func (c *Client) do(ctx context.Context, method, path string, body, dest interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Honeycomb-Team", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "grafana-honeycomb-datasource/"+pluginVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	c.logRateLimitHeaders(resp.Header, path)

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return &APIError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(rawBody)),
			RetryAfter: retryAfter,
		}
	}

	if dest != nil && len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, dest); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}

// doWithRetry wraps do with exponential-backoff retry for transient errors
// (429 and 5xx). It does NOT apply to POST /query_results.
func (c *Client) doWithRetry(ctx context.Context, method, path string, body, dest interface{}) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := c.do(ctx, method, path, body, dest)
		if err == nil {
			return nil
		}
		lastErr = err

		if !IsRateLimit(err) && !IsServerError(err) {
			return err
		}

		if attempt == maxRetries {
			break
		}

		delay := backoffDelay(attempt, err)
		c.logger.Debug("Retrying Honeycomb request",
			"attempt", attempt+1,
			"method", method,
			"path", path,
			"delay_ms", delay.Milliseconds(),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

// logRateLimitHeaders logs Honeycomb rate limit header info at appropriate levels.
func (c *Client) logRateLimitHeaders(headers http.Header, path string) {
	rl := headers.Get("RateLimit")
	if rl == "" {
		return
	}
	info := ParseRateLimitHeader(rl)
	if info.Remaining < 3 {
		c.logger.Warn("Honeycomb rate limit nearly exhausted",
			"limit", info.Limit,
			"remaining", info.Remaining,
			"reset_in_seconds", info.Reset.Seconds(),
			"path", path,
		)
	} else {
		c.logger.Debug("Honeycomb rate limit status",
			"remaining", info.Remaining,
			"reset_in_seconds", info.Reset.Seconds(),
		)
	}
}

// backoffDelay computes the wait duration for a retry attempt.
// Uses Retry-After if present; otherwise exponential backoff with jitter.
func backoffDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if stderrors.As(err, &apiErr) && apiErr.RetryAfter != nil {
		if d := time.Until(*apiErr.RetryAfter); d > 0 {
			return d
		}
	}

	base := float64(retryBaseDelay) * math.Pow(2, float64(attempt))
	jitter := base * retryJitterFactor * rand.Float64() //nolint:gosec
	d := time.Duration(base + jitter)
	if d > retryMaxDelay {
		return retryMaxDelay
	}
	return d
}
