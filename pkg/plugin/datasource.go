// Package plugin implements the Grafana backend data source for Honeycomb.
//
// Architecture summary:
//   - One Datasource instance per configured data source in Grafana.
//   - All Honeycomb API calls go through the backend; no secrets reach the browser.
//   - A three-level TTL cache + singleflight protects the 10 req/min limit on
//     Create Query Result (see ADR-002 and ADR-003).
//   - Frames are annotated with deep links back to the Honeycomb UI (see ADR-004).
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/cache"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/fingerprint"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/ratelimit"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/transform"
)

// Ensure Datasource satisfies the required interfaces at compile time.
var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// datasourceSettings is the non-secret configuration stored in jsonData.
type datasourceSettings struct {
	APIURL string `json:"apiUrl"`

	// Team and Environment power deep links into ui.honeycomb.io. Both empty =
	// fall back to the query URL Honeycomb returns alongside results.
	Team        string `json:"team"`
	Environment string `json:"environment"`

	// TimeWindowDays clamps query time ranges before they reach Honeycomb.
	// 0 = unbounded. Default is defaultTimeWindowDays.
	TimeWindowDays int `json:"timeWindowDays"`

	CacheTTLL1Minutes int `json:"cacheTtlL1Minutes"`
	CacheTTLL2Minutes int `json:"cacheTtlL2Minutes"`
	CacheTTLL3Minutes int `json:"cacheTtlL3Minutes"`
}

// defaultTimeWindowDays mirrors the frontend default (DEFAULT_TIME_WINDOW_DAYS).
const defaultTimeWindowDays = 7

// Datasource is a single configured Honeycomb data source instance.
// Grafana creates one Datasource per configured data source; they are
// replaced on settings changes.
type Datasource struct {
	uid      string
	settings datasourceSettings
	client   *honeycomb.Client
	cache    *cache.Cache
	sfGroup  cache.Group
	limiter  *ratelimit.Limiter
	logger   log.Logger

	ttlL1 time.Duration
	ttlL2 time.Duration
	ttlL3 time.Duration
}

// NewDatasource is called by Grafana whenever a data source instance is
// created or updated. It initializes the HTTP client from the (decrypted)
// secure settings and allocates per-instance cache and rate limiter.
func NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	logger := log.DefaultLogger.With("datasource_uid", settings.UID, "datasource_name", settings.Name)

	// Decode non-secret settings.
	var dsSettings datasourceSettings
	if len(settings.JSONData) > 0 {
		if err := json.Unmarshal(settings.JSONData, &dsSettings); err != nil {
			return nil, fmt.Errorf("decode datasource settings: %w", err)
		}
	}

	// Retrieve API key from secure storage.
	apiKey := settings.DecryptedSecureJSONData["apiKey"]

	client, err := honeycomb.New(honeycomb.Config{
		APIURL: dsSettings.APIURL,
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create honeycomb client: %w", err)
	}

	if dsSettings.TimeWindowDays == 0 {
		dsSettings.TimeWindowDays = defaultTimeWindowDays
	}

	logger.Debug("Honeycomb datasource initialized",
		"apiUrl", dsSettings.APIURL,
		"team", dsSettings.Team,
		"environment", dsSettings.Environment,
		"timeWindowDays", dsSettings.TimeWindowDays,
	)

	ttlL1 := cache.DefaultTTLQueryID
	if dsSettings.CacheTTLL1Minutes > 0 {
		ttlL1 = time.Duration(dsSettings.CacheTTLL1Minutes) * time.Minute
	}
	ttlL2 := cache.DefaultTTLQueryResultID
	if dsSettings.CacheTTLL2Minutes > 0 {
		ttlL2 = time.Duration(dsSettings.CacheTTLL2Minutes) * time.Minute
	}
	ttlL3 := cache.DefaultTTLCompletedResult
	if dsSettings.CacheTTLL3Minutes > 0 {
		ttlL3 = time.Duration(dsSettings.CacheTTLL3Minutes) * time.Minute
	}

	return &Datasource{
		uid:      settings.UID,
		settings: dsSettings,
		client:   client,
		cache:    cache.New(5 * time.Minute), // janitor interval
		limiter:  ratelimit.New(),
		logger:   logger,
		ttlL1:    ttlL1,
		ttlL2:    ttlL2,
		ttlL3:    ttlL3,
	}, nil
}

// Dispose releases resources held by the datasource instance.
// Called by Grafana when the data source is removed or updated.
func (d *Datasource) Dispose() {
	d.cache.Stop()
}

// ---------------------------------------------------------------------------
// QueryData – main query execution path
// ---------------------------------------------------------------------------

// QueryData handles all panel queries. Each query in the request is executed
// concurrently (up to a small concurrency cap) and collected into a response.
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	// Fan out queries concurrently with a bounded goroutine pool.
	const maxConcurrency = 10
	sem := make(chan struct{}, maxConcurrency)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, q := range req.Queries {
		// Skip hidden queries before spawning goroutines.
		// The "hide" flag is set by Grafana in the query JSON when a user hides a
		// panel's query; backend.DataQuery doesn't expose it as a struct field.
		if isHidden(q.JSON) {
			continue
		}

		wg.Add(1)
		q := q // capture loop variable
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := d.runQuery(ctx, q)
			mu.Lock()
			response.Responses[q.RefID] = res
			mu.Unlock()
		}()
	}

	wg.Wait()
	return response, nil
}

// runQuery executes a single panel query and returns the Grafana response.
func (d *Datasource) runQuery(ctx context.Context, gq backend.DataQuery) backend.DataResponse {
	pq, err := parseQuery(gq.JSON)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("invalid query: %v", err))
	}

	// Skip empty queries (freshly-added panels before the user has filled in the editor).
	if pq.IsEmpty() {
		return backend.DataResponse{Frames: data.Frames{}}
	}

	if err := pq.Validate(); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("query validation: %v", err))
	}

	// Dispatch SLO queries to a separate path that uses /1/slos/* rather than
	// the events query API. SLO results have their own caching keys (no
	// rate-limit pressure on Create Query Result).
	if pq.QueryType == QueryTypeSLO {
		return d.runSLOQuery(ctx, pq)
	}

	// Logs and Traces both build a Query Data API call under the hood but
	// with very different shapes (no calc + breakdowns of attrs for logs;
	// filtered breakdown of span fields for traces). Route to dedicated
	// builders that produce the right HoneycombQuery and frame transformer.
	if pq.QueryType == QueryTypeLogs {
		return d.runLogsQuery(ctx, gq, pq)
	}
	if pq.QueryType == QueryTypeTraces {
		return d.runTracesQuery(ctx, gq, pq)
	}

	// Build the base Honeycomb query (without time range — applied separately
	// so L1 cache (query_id) is time-independent).
	hq, err := pq.ToHoneycombQuery()
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("build honeycomb query: %v", err))
	}

	// Apply time range, clamping by the configured time window if necessary.
	// MaxDataPoints comes from Grafana's panel width — passing it lets the
	// auto-granularity match chart fidelity instead of always sampling at
	// ~100 buckets.
	from := clampFrom(gq.TimeRange.From, gq.TimeRange.To, d.settings.TimeWindowDays)
	to := gq.TimeRange.To
	fingerprint.ApplyTimeRange(&hq, from, to, pq.Granularity, gq.MaxDataPoints)

	execSpec := fingerprint.ExecutionSpec{
		QuerySpec: fingerprint.QuerySpec{
			DatasourceUID: d.uid,
			Dataset:       pq.Dataset,
			Query:         hq,
		},
		From:          from.Unix(),
		To:            to.Unix(),
		DisableSeries: pq.ShouldDisableSeries(),
		Limit:         pq.Limit,
	}
	execKey := fingerprint.ExecutionKey(execSpec)

	// Singleflight: collapse concurrent identical requests into one execution.
	result, err, _ := d.sfGroup.Do(execKey, func() (interface{}, error) {
		return d.executeQuery(ctx, pq, hq, execSpec, execKey)
	})
	if err != nil {
		d.logger.Error("Query execution failed",
			"dataset", pq.Dataset,
			"error", err,
		)
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("execute query: %v", err))
	}

	qr := result.(*honeycomb.QueryResultResponse)
	frames, err := transform.ToFrames(qr, transform.FrameOptions{
		Mode:        pq.FrameMode(),
		QueryURL:    qr.Links.QueryURL,
		GraphURL:    qr.Links.GraphImageURL,
		MaxGroups:   transform.DefaultMaxGroups,
		APIURL:      d.settings.APIURL,
		Team:        d.settings.Team,
		Environment: d.settings.Environment,
		Dataset:     pq.Dataset,
	})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("transform results: %v", err))
	}

	return backend.DataResponse{Frames: frames}
}

// executeQuery implements the three-level cache lookup + Honeycomb API flow.
//
//	L3 hit  → return cached completed result immediately
//	L2 hit  → poll Get Query Result (already have result_id)
//	L1 hit  → have query_id, submit Create Query Result (rate-limited), then poll
//	cold    → Create Query, Create Query Result (rate-limited), then poll
func (d *Datasource) executeQuery(
	ctx context.Context,
	pq HoneycombQuery,
	hq honeycomb.Query,
	spec fingerprint.ExecutionSpec,
	execKey string,
) (*honeycomb.QueryResultResponse, error) {
	// --- L3: Check for a completed cached result ---
	if v, ok := d.cache.Get(fingerprint.CompletedResultKey(execKey)); ok {
		d.logger.Debug("L3 cache hit (completed result)", "dataset", pq.Dataset, "key", execKey[:8])
		return v.(*honeycomb.QueryResultResponse), nil
	}

	// --- L1: Get or create the Honeycomb query_id ---
	shapeKey := fingerprint.QueryShapeKey(spec.QuerySpec)
	queryID, err := d.getOrCreateQueryID(ctx, pq.Dataset, hq, shapeKey)
	if err != nil {
		return nil, fmt.Errorf("get query ID: %w", err)
	}

	// --- L2: Get or create the query_result_id ---
	queryResultID, err := d.getOrCreateQueryResultID(ctx, pq.Dataset, queryID, pq, execKey)
	if err != nil {
		return nil, fmt.Errorf("get query result ID: %w", err)
	}

	// --- Poll until complete ---
	result, err := d.client.GetQueryResult(ctx, pq.Dataset, queryResultID)
	if err != nil {
		// Evict the result_id from L2 so the next call retries.
		d.cache.Delete(execKey + ":resultid")
		return nil, fmt.Errorf("poll query result: %w", err)
	}

	// --- Store in L3 cache ---
	d.cache.Set(fingerprint.CompletedResultKey(execKey), result, d.ttlL3)
	d.logger.Debug("Query complete, cached in L3",
		"dataset", pq.Dataset,
		"result_id", queryResultID,
		"ttl", d.ttlL3.String(),
	)

	return result, nil
}

// getOrCreateQueryID returns a cached query_id or creates a new one via the
// Honeycomb Queries API.
func (d *Datasource) getOrCreateQueryID(ctx context.Context, dataset string, hq honeycomb.Query, shapeKey string) (string, error) {
	cacheKey := "queryid:" + shapeKey
	if v, ok := d.cache.Get(cacheKey); ok {
		d.logger.Debug("L1 cache hit (query_id)", "dataset", dataset, "shape_key", shapeKey[:8])
		return v.(string), nil
	}

	queryID, err := d.client.CreateQuery(ctx, dataset, hq)
	if err != nil {
		return "", err
	}
	d.cache.Set(cacheKey, queryID, d.ttlL1)
	d.logger.Debug("Created query, cached in L1", "dataset", dataset, "query_id", queryID)
	return queryID, nil
}

// getOrCreateQueryResultID returns a cached query_result_id or submits a new
// Create Query Result call (rate-limited via the token bucket).
func (d *Datasource) getOrCreateQueryResultID(ctx context.Context, dataset, queryID string, pq HoneycombQuery, execKey string) (string, error) {
	cacheKey := execKey + ":resultid"
	if v, ok := d.cache.Get(cacheKey); ok {
		d.logger.Debug("L2 cache hit (query_result_id)", "dataset", dataset, "exec_key", execKey[:8])
		return v.(string), nil
	}

	// Acquire a rate-limit token before calling Create Query Result.
	if err := d.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limit: %w", err)
	}

	req := honeycomb.QueryResultRequest{
		QueryID:                 queryID,
		DisableSeries:           pq.ShouldDisableSeries(),
		DisableTotalByAggregate: true,
		DisableOtherByAggregate: true,
	}
	if pq.Limit > 0 {
		req.Limit = pq.Limit
	}

	resp, err := d.client.CreateQueryResult(ctx, dataset, req)
	if err != nil {
		if honeycomb.IsRateLimit(err) {
			// On 429 from Create Query Result (no Retry-After per Honeycomb docs),
			// return a user-visible error. The next dashboard refresh will hit L1
			// (query_id still cached) and try again with a fresh token.
			return "", fmt.Errorf("honeycomb rate limit exceeded (Create Query Result); "+
				"too many concurrent queries — check ADR-003 for mitigation options: %w", err)
		}
		return "", err
	}

	d.cache.Set(cacheKey, resp.ID, d.ttlL2)
	d.logger.Debug("Created query result, cached in L2",
		"dataset", dataset,
		"query_result_id", resp.ID,
	)
	return resp.ID, nil
}

// ---------------------------------------------------------------------------
// CheckHealth
// ---------------------------------------------------------------------------

// clampFrom returns from clamped so that to-from never exceeds windowDays.
// windowDays <= 0 disables clamping. If from is already within the window
// (or after to, which Grafana shouldn't allow), it is returned unchanged.
func clampFrom(from, to time.Time, windowDays int) time.Time {
	if windowDays <= 0 {
		return from
	}
	maxRange := time.Duration(windowDays) * 24 * time.Hour
	earliest := to.Add(-maxRange)
	if from.Before(earliest) {
		return earliest
	}
	return from
}

// isHidden parses the "hide" flag from the raw query JSON. Grafana sets this
// when a user toggles the eye icon in the query editor.
func isHidden(rawJSON json.RawMessage) bool {
	if len(rawJSON) == 0 {
		return false
	}
	var base struct {
		Hide bool `json:"hide"`
	}
	if err := json.Unmarshal(rawJSON, &base); err != nil {
		return false
	}
	return base.Hide
}

// CheckHealth verifies that the data source can reach Honeycomb and that the
// configured API key is valid. Called by Grafana when the user tests the
// data source configuration.
func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if err := d.client.HealthCheck(ctx); err != nil {
		d.logger.Error("Health check failed", "error", err)
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("Cannot connect to Honeycomb: %v", err),
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Connected to Honeycomb successfully",
	}, nil
}
