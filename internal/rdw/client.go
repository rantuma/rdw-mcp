package rdw

import (
	"container/list"
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// jitterDivisor controls the relative magnitude of the random jitter applied
// to exponential backoff (delay / jitterDivisor on each side, i.e. ±25%).
const jitterDivisor = 4

type (
	// ClientConfig controls retry, per-call timeout and response-cache behaviour
	// of doRDWGet. The zero value is safe — defaults are applied as needed.
	ClientConfig struct {
		// MaxAttempts is the total number of attempts (initial request + retries)
		// for transient errors. Zero -> default. Set to 1 to disable retries.
		MaxAttempts int
		// BaseBackoff is the initial exponential backoff delay. Zero -> default.
		BaseBackoff time.Duration
		// MaxBackoff caps the per-retry sleep. Zero -> default.
		MaxBackoff time.Duration
		// PerCallTimeout bounds a single endpoint call (including retries).
		// Zero -> default.
		PerCallTimeout time.Duration
		// CacheTTL is the TTL for entries in the response cache. Zero -> default.
		// A negative TTL disables the cache.
		CacheTTL time.Duration
		// CacheSize is the LRU capacity in entries. Zero -> default. Negative
		// disables the cache.
		CacheSize int
	}

	cacheEntry struct {
		key       string
		body      []byte
		expiresAt time.Time
	}

	// responseCache is a simple thread-safe LRU with per-entry TTL.
	responseCache struct {
		mu       sync.Mutex
		capacity int
		items    map[string]*list.Element
		order    *list.List
	}
)

// activeConfig holds the runtime configuration. Mutated via SetClientConfig
// from main at startup. Reads are protected by configMu.
//
//nolint:gochecknoglobals // intentional process-wide RDW client tuning
var (
	configMu      sync.RWMutex
	activeConfig  = ClientConfig{}
	activeCache   *responseCache
	activeCacheMu sync.Mutex
)

// SetClientConfig replaces the active RDW client configuration. Safe to call
// concurrently. Resetting the cache size or TTL rebuilds the cache.
//
// A negative CacheTTL or CacheSize disables caching. The zero value leaves
// caching disabled — main must opt in by calling SetClientConfig with positive
// CacheTTL/CacheSize. This keeps tests deterministic without requiring them to
// reset shared state.
func SetClientConfig(cfg ClientConfig) {
	configMu.Lock()
	activeConfig = cfg
	configMu.Unlock()

	activeCacheMu.Lock()
	defer activeCacheMu.Unlock()

	if cfg.CacheTTL <= 0 || cfg.CacheSize <= 0 {
		activeCache = nil
		return
	}

	activeCache = newResponseCache(cfg.CacheSize)
}

// getConfig returns the active configuration with defaults filled in.
func getConfig() ClientConfig {
	configMu.RLock()
	cfg := activeConfig
	configMu.RUnlock()

	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}

	if cfg.BaseBackoff == 0 {
		cfg.BaseBackoff = DefaultBaseBackoff
	}

	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = DefaultMaxBackoff
	}

	if cfg.PerCallTimeout == 0 {
		cfg.PerCallTimeout = DefaultPerCallTimeout
	}

	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = DefaultCacheTTL
	}

	if cfg.CacheSize == 0 {
		cfg.CacheSize = DefaultCacheSize
	}

	return cfg
}

// getCache returns the active response cache, or nil when caching is disabled
// (the default). main enables caching via SetClientConfig.
func getCache() *responseCache {
	activeCacheMu.Lock()
	defer activeCacheMu.Unlock()

	return activeCache
}

// ResetCache clears the active response cache. Intended for tests.
func ResetCache() {
	activeCacheMu.Lock()
	defer activeCacheMu.Unlock()

	if activeCache != nil {
		activeCache.purge()
	}
}

// errRetryable signals a transient error to the retry loop. Wrap any error
// with %w of errRetryable to opt into retries.
var errRetryable = errors.New("retryable rdw error")

// isRetryableStatus returns true for HTTP statuses that should be retried.
func isRetryableStatus(status int) bool {
	if status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests {
		return true
	}

	return status >= http.StatusInternalServerError
}

// computeBackoff returns the sleep duration for attempt n (0-based), with jitter.
func computeBackoff(attempt int, base, maxBackoff time.Duration) time.Duration {
	delay := base << attempt
	if delay <= 0 || delay > maxBackoff {
		delay = maxBackoff
	}

	// Add up to ±25 % jitter.
	jitterRange := int64(delay / jitterDivisor)
	if jitterRange > 0 {
		jitter := pseudoJitter(jitterRange)
		delay += time.Duration(jitter)
	}

	return delay
}

// sleepCtx sleeps for d or returns ctx.Err() when the context is cancelled
// first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ---------------------------------------------------------------------------
// In-memory TTL LRU cache for raw response bodies keyed by URL.
// ---------------------------------------------------------------------------

func newResponseCache(capacity int) *responseCache {
	if capacity <= 0 {
		capacity = DefaultCacheSize
	}

	return &responseCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

func (cache *responseCache) get(key string) ([]byte, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	elem, ok := cache.items[key]
	if !ok {
		return nil, false
	}

	entry, _ := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		cache.order.Remove(elem)
		delete(cache.items, key)

		return nil, false
	}

	cache.order.MoveToFront(elem)

	return entry.body, true
}

func (cache *responseCache) set(
	key string,
	body []byte,
	ttl time.Duration,
) {
	if ttl <= 0 {
		return
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	if elem, ok := cache.items[key]; ok {
		entry, _ := elem.Value.(*cacheEntry)
		entry.body = body
		entry.expiresAt = time.Now().Add(ttl)
		cache.order.MoveToFront(elem)

		return
	}

	entry := &cacheEntry{key: key, body: body, expiresAt: time.Now().Add(ttl)}
	elem := cache.order.PushFront(entry)
	cache.items[key] = elem

	for cache.order.Len() > cache.capacity {
		oldest := cache.order.Back()
		if oldest == nil {
			break
		}

		cache.order.Remove(oldest)
		old, _ := oldest.Value.(*cacheEntry)
		delete(cache.items, old.key)
	}
}

func (cache *responseCache) purge() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.items = make(map[string]*list.Element, cache.capacity)
	cache.order.Init()
}
