package ginx

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

/*
Package ginx provides rate limiting middleware for Gin web framework.

This package implements two complementary rate limiting strategies:
1. Token bucket algorithm (RateLimit) for smooth, second-based rate limiting
2. Fixed window counters (RateLimitPerMinute/Hour/Day) for quota management

Key Features:
  - Token bucket algorithm for smooth rate limiting with burst support
  - Fixed window algorithm for minute/hour/day quotas
  - Configurable storage backends (memory included, Redis support via interface)
  - Per-IP, per-user, and custom key-based rate limiting
  - HTTP header support (X-RateLimit-* headers)
  - Dynamic rate limiting with per-key limits
  - Waiting middleware variant for traffic smoothing
  - Thread-safe with automatic cleanup of expired limiters
  - Composable: combine multiple rate limiters for layered protection

Basic Usage:

	// Simple IP-based rate limiting: 100 rps, burst of 200 (default behavior)
	r.Use(ginx.RateLimit(100, 200))

	// Per-minute rate limiting: 60 requests per minute
	r.Use(ginx.RateLimitPerMinute(60))

	// Per-user rate limiting (requires user_id in context)
	r.Use(ginx.RateLimit(50, 100, ginx.WithUser()))

	// Per-path rate limiting (different limits per endpoint)
	r.Use(ginx.RateLimit(10, 20, ginx.WithPath()))

Advanced Usage:

	// Multiple options combined
	r.Use(ginx.RateLimit(100, 200,
		ginx.WithUser(),
		ginx.WithStore(redisStore),
		ginx.WithSkipFunc(skipAdmins),
		ginx.WithoutHeaders()))

	// Rate limiting with wait (smooths traffic spikes)
	r.Use(ginx.RateLimit(50, 100, ginx.WithWait(5*time.Second)))

	// Dynamic per-user limits based on user plan
	r.Use(ginx.RateLimit(0, 0,
		ginx.WithUser(),
		ginx.WithDynamicLimits(func(userID string) (rps, burst int) {
			if isPremium(userID) {
				return 1000, 2000
			}
			return 100, 200
		})))

	// Combine RPS and time window limits for layered protection
	r.Use(ginx.NewChain().
		Use(ginx.RateLimit(10, 20)).        // Prevent bursts
		Use(ginx.RateLimitPerHour(1000)).   // Quota management
		Build())

Resource Management:

All stores (both default shared and custom stores) are automatically managed.
Use CleanupRateLimiters() at application shutdown for comprehensive cleanup.

Thread Safety:

All components are thread-safe and designed for high-concurrency environments.
The memory store includes automatic cleanup to prevent memory leaks.
*/

// ============================================================================
// Rate Limiting - Simplified and High-Performance Design
// ============================================================================

// TimeWindow represents different time window types for rate limiting
type TimeWindow int

const (
	TimeWindowSecond TimeWindow = iota // Per second (default token bucket)
	TimeWindowMinute                   // Per minute (sliding window)
	TimeWindowHour                     // Per hour (sliding window)
	TimeWindowDay                      // Per day (sliding window)
)

// RateLimitStore defines the interface for storing and managing rate limiters.
// It provides methods to store, retrieve, and manage rate.Limiter instances by key.
type RateLimitStore interface {
	// Get returns the limiter for the given key
	Get(key string) (*rate.Limiter, bool)
	// Set stores the limiter for the given key
	Set(key string, limiter *rate.Limiter)
	// Delete removes the limiter for the given key
	Delete(key string)
	// Clear removes all expired limiters
	Clear()
	// Close cleans up resources
	Close() error
}

// WindowCounterStore defines the interface for storing time-window based counters.
// Used for minute/hour/day rate limiting with fixed window algorithm.
type WindowCounterStore interface {
	// Increment increments the counter for the given key and window, returns new count
	Increment(key string, window time.Time) (int64, error)
	// IncrementWithinLimit atomically increments the counter when under limit.
	// It returns the resulting count, whether the increment happened, and any errors.
	IncrementWithinLimit(key string, window time.Time, limit int64) (count int64, allowed bool, err error)
	// Get returns the current count for the given key and window
	Get(key string, window time.Time) (int64, error)
	// Clear removes expired counters
	Clear()
	// Close cleans up resources
	Close() error
}

// MemoryLimiterStore provides a thread-safe, in-memory implementation of RateLimitStore.
// It automatically cleans up expired limiters to prevent memory leaks and is registered
// globally for automatic resource management.
type MemoryLimiterStore struct {
	mu         sync.RWMutex
	limiters   map[string]*rate.Limiter
	lastAccess map[string]time.Time
	maxIdle    time.Duration

	// Cleanup goroutine control
	ticker    *time.Ticker
	done      chan struct{}
	closeOnce sync.Once
}

// MemoryWindowCounterStore provides a thread-safe, in-memory implementation of WindowCounterStore.
// It uses a fixed window algorithm for time-based rate limiting (minute/hour/day).
type MemoryWindowCounterStore struct {
	mu         sync.RWMutex
	counters   map[string]int64     // key:window -> count
	lastAccess map[string]time.Time // key:window -> last access time
	maxIdle    time.Duration

	// Cleanup goroutine control
	ticker    *time.Ticker
	done      chan struct{}
	closeOnce sync.Once
}

var (
	// Global registry of all active stores for automatic cleanup
	activeStores      = make(map[RateLimitStore]struct{})
	activeStoresMutex sync.RWMutex

	// Global registry of window counter stores
	activeWindowStores      = make(map[WindowCounterStore]struct{})
	activeWindowStoresMutex sync.RWMutex

	// Global default store shared by all rate limiters
	defaultStore     RateLimitStore
	defaultStoreOnce sync.Once

	// Global default window counter store
	defaultWindowStore     WindowCounterStore
	defaultWindowStoreOnce sync.Once
)

// NewMemoryLimiterStore creates a thread-safe in-memory store with automatic cleanup.
//
// Parameters:
//   - maxIdle: Duration to keep unused limiters (defaults to 5 minutes if <= 0)
//
// Resource Management:
// The store is automatically registered globally and cleaned up by CleanupRateLimiters().
// Manual Close() is optional unless immediate cleanup is needed.
func NewMemoryLimiterStore(maxIdle time.Duration) RateLimitStore {
	if maxIdle <= 0 {
		maxIdle = 5 * time.Minute
	}

	store := &MemoryLimiterStore{
		limiters:   make(map[string]*rate.Limiter),
		lastAccess: make(map[string]time.Time),
		maxIdle:    maxIdle,
		done:       make(chan struct{}),
	}

	// Start cleanup goroutine
	store.ticker = time.NewTicker(maxIdle / 2)
	go store.cleanup()

	// Register for automatic cleanup
	activeStoresMutex.Lock()
	activeStores[store] = struct{}{}
	activeStoresMutex.Unlock()

	return store
}

// Get retrieves a rate limiter for the given key and updates its last access time.
func (s *MemoryLimiterStore) Get(key string) (*rate.Limiter, bool) {
	s.mu.Lock()
	limiter, exists := s.limiters[key]
	if exists {
		s.lastAccess[key] = time.Now()
	}
	s.mu.Unlock()
	return limiter, exists
}

// Set stores a rate limiter for the given key and records the access time.
func (s *MemoryLimiterStore) Set(key string, limiter *rate.Limiter) {
	s.mu.Lock()
	s.limiters[key] = limiter
	s.lastAccess[key] = time.Now()
	s.mu.Unlock()
}

// Delete removes a rate limiter and its access time record.
func (s *MemoryLimiterStore) Delete(key string) {
	s.mu.Lock()
	delete(s.limiters, key)
	delete(s.lastAccess, key)
	s.mu.Unlock()
}

// Clear removes all stored rate limiters and access time records.
func (s *MemoryLimiterStore) Clear() {
	s.mu.Lock()
	s.limiters = make(map[string]*rate.Limiter)
	s.lastAccess = make(map[string]time.Time)
	s.mu.Unlock()
}

// Close stops the cleanup goroutine and releases resources.
func (s *MemoryLimiterStore) Close() error {
	s.closeOnce.Do(func() {
		s.ticker.Stop()
		close(s.done)
		s.Clear()
		// Unregister from global cleanup
		activeStoresMutex.Lock()
		delete(activeStores, s)
		activeStoresMutex.Unlock()
	})
	return nil
}

// cleanup runs in a separate goroutine to remove expired rate limiters.
func (s *MemoryLimiterStore) cleanup() {
	for {
		select {
		case <-s.done:
			return
		case now := <-s.ticker.C:
			s.mu.Lock()
			for key, lastAccess := range s.lastAccess {
				if now.Sub(lastAccess) > s.maxIdle {
					delete(s.limiters, key)
					delete(s.lastAccess, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// NewMemoryWindowCounterStore creates a thread-safe in-memory window counter store with automatic cleanup.
//
// Parameters:
//   - maxIdle: Duration to keep unused counters (defaults to 25 hours if <= 0, sufficient for daily limits)
//
// Resource Management:
// The store is automatically registered globally and cleaned up by CleanupRateLimiters().
func NewMemoryWindowCounterStore(maxIdle time.Duration) WindowCounterStore {
	if maxIdle <= 0 {
		maxIdle = 25 * time.Hour // Default: keep counters for just over a day
	}

	store := &MemoryWindowCounterStore{
		counters:   make(map[string]int64),
		lastAccess: make(map[string]time.Time),
		maxIdle:    maxIdle,
		done:       make(chan struct{}),
	}

	// Start cleanup goroutine
	store.ticker = time.NewTicker(maxIdle / 2)
	go store.cleanupWindows()

	// Register for automatic cleanup
	activeWindowStoresMutex.Lock()
	activeWindowStores[store] = struct{}{}
	activeWindowStoresMutex.Unlock()

	return store
}

// Increment increments the counter for the given key and window, returns new count
func (s *MemoryWindowCounterStore) Increment(key string, window time.Time) (int64, error) {
	windowKey := formatWindowKey(key, window)
	s.mu.Lock()
	count := s.counters[windowKey] + 1
	s.counters[windowKey] = count
	s.lastAccess[windowKey] = time.Now()
	s.mu.Unlock()
	return count, nil
}

// Get returns the current count for the given key and window
func (s *MemoryWindowCounterStore) Get(key string, window time.Time) (int64, error) {
	windowKey := formatWindowKey(key, window)
	s.mu.RLock()
	count := s.counters[windowKey]
	s.mu.RUnlock()
	return count, nil
}

// IncrementWithinLimit atomically increments the current window counter when under limit.
func (s *MemoryWindowCounterStore) IncrementWithinLimit(key string, window time.Time, limit int64) (int64, bool, error) {
	windowKey := formatWindowKey(key, window)
	s.mu.Lock()
	defer s.mu.Unlock()

	count := s.counters[windowKey]
	if limit > 0 && count >= limit {
		return count, false, nil
	}

	count++
	s.counters[windowKey] = count
	s.lastAccess[windowKey] = time.Now()
	return count, true, nil
}

// Clear removes all stored counters and access time records.
func (s *MemoryWindowCounterStore) Clear() {
	s.mu.Lock()
	s.counters = make(map[string]int64)
	s.lastAccess = make(map[string]time.Time)
	s.mu.Unlock()
}

// Close stops the cleanup goroutine and releases resources.
func (s *MemoryWindowCounterStore) Close() error {
	s.closeOnce.Do(func() {
		s.ticker.Stop()
		close(s.done)
		s.Clear()
		// Unregister from global cleanup
		activeWindowStoresMutex.Lock()
		delete(activeWindowStores, s)
		activeWindowStoresMutex.Unlock()
	})
	return nil
}

// cleanupWindows runs in a separate goroutine to remove expired window counters.
func (s *MemoryWindowCounterStore) cleanupWindows() {
	for {
		select {
		case <-s.done:
			return
		case now := <-s.ticker.C:
			s.mu.Lock()
			for key, lastAccess := range s.lastAccess {
				if now.Sub(lastAccess) > s.maxIdle {
					delete(s.counters, key)
					delete(s.lastAccess, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// formatWindowKey creates a unique key for a time window
func formatWindowKey(key string, window time.Time) string {
	return fmt.Sprintf("%s:%d", key, window.Unix())
}

// ============================================================================
// Rate Limiting Middleware
// ============================================================================

// rateLimiter represents a configurable rate limiting middleware.
// It uses the token bucket algorithm via golang.org/x/time/rate for precise rate limiting.
type rateLimiter struct {
	store            RateLimitStore
	windowStore      WindowCounterStore
	rps              int
	burst            int
	limit            int        // For time-window based limiting (requests per window)
	window           TimeWindow // Time window type
	keyFunc          func(*gin.Context) string
	skipFunc         func(*gin.Context) bool
	headers          bool
	retryAfterHeader bool                                  // Controls Retry-After header independently from X-RateLimit-* headers
	waitTimeout      time.Duration                         // 0 means no waiting, >0 enables wait mode
	dynamicLimits    func(key string) (rps int, burst int) // nil means static limits, non-nil enables dynamic limits
	dynamicWindow    func(key string) int                  // For window-based rate limiting dynamic limits
}

// newRateLimiter creates a new rate limiter with the specified requests per second (rps) and burst capacity.
func newRateLimiter(rps, burst int) *rateLimiter {
	return &rateLimiter{
		store:            nil, // Will be lazily initialized in getLimiter()
		windowStore:      nil, // Will be lazily initialized for window-based limiting
		rps:              rps,
		burst:            burst,
		limit:            0,                // Not used for token bucket mode
		window:           TimeWindowSecond, // Default to per-second limiting
		keyFunc:          defaultKeyFunc,
		headers:          true,
		retryAfterHeader: true, // Enable Retry-After by default
	}
}

// newWindowRateLimiter creates a new time-window based rate limiter
func newWindowRateLimiter(limit int, window TimeWindow) *rateLimiter {
	return &rateLimiter{
		store:            nil,
		windowStore:      nil, // Will be lazily initialized
		rps:              0,
		burst:            0,
		limit:            limit,
		window:           window,
		keyFunc:          defaultKeyFunc,
		headers:          true,
		retryAfterHeader: true,
	}
}

// Middleware returns a Gin middleware function that enforces rate limiting.
// The middleware checks the rate limit for each request and either allows it to proceed
// or returns a 429 Too Many Requests response.
// If waitTimeout is set, it will wait for available tokens instead of immediately rejecting.
func (rl *rateLimiter) Middleware() Middleware {
	// Use window-based limiting for minute/hour/day
	if rl.window != TimeWindowSecond {
		return rl.windowMiddleware()
	}

	if rl.waitTimeout > 0 {
		return rl.waitMiddleware()
	}
	return rl.standardMiddleware()
}

// windowMiddleware implements time-window based rate limiting (minute/hour/day)
func (rl *rateLimiter) windowMiddleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if rl.skipFunc != nil && rl.skipFunc(c) {
				next(c)
				return
			}

			key := rl.getKey(c)

			// Lazy initialization of default window store
			if rl.windowStore == nil {
				defaultWindowStoreOnce.Do(func() {
					defaultWindowStore = NewMemoryWindowCounterStore(25 * time.Hour)
				})
				rl.windowStore = defaultWindowStore
			}

			// Get current window
			window := rl.getCurrentWindow(time.Now())

			// Get limit (static or dynamic)
			limit := rl.getWindowLimit(key)

			count, allowed, err := rl.windowStore.IncrementWithinLimit(key, window, int64(limit))
			if err != nil {
				// On store failure, respond with 500 to avoid silent drops
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "rate limiter store failure",
				})
				return
			}

			if !allowed {
				rl.handleWindowRateLimit(c, window, count, limit)
				return
			}

			if rl.headers {
				rl.setWindowHeaders(c, count, window, limit)
			}

			next(c)
		}
	}
}

// getCurrentWindow returns the current time window start time
func (rl *rateLimiter) getCurrentWindow(now time.Time) time.Time {
	switch rl.window {
	case TimeWindowMinute:
		return now.Truncate(time.Minute)
	case TimeWindowHour:
		return now.Truncate(time.Hour)
	case TimeWindowDay:
		year, month, day := now.Date()
		return time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	default:
		return now.Truncate(time.Second)
	}
}

// getWindowDuration returns the duration of the current window type
func (rl *rateLimiter) getWindowDuration() time.Duration {
	switch rl.window {
	case TimeWindowMinute:
		return time.Minute
	case TimeWindowHour:
		return time.Hour
	case TimeWindowDay:
		return 24 * time.Hour
	default:
		return time.Second
	}
}

// handleWindowRateLimit processes a rate-limited request for window-based limiting
func (rl *rateLimiter) handleWindowRateLimit(c *gin.Context, window time.Time, count int64, limit int) {
	// Calculate time until next window
	windowDuration := rl.getWindowDuration()
	nextWindow := window.Add(windowDuration)
	retryAfter := max(int64(time.Until(nextWindow).Seconds()), 1)

	if rl.headers {
		rl.setWindowHeaders(c, count, window, limit)
	}

	if rl.retryAfterHeader {
		c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
	}

	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":       "rate limit exceeded",
		"retry_after": retryAfter,
	})
}

// setWindowHeaders adds X-RateLimit-* headers for window-based rate limiting
func (rl *rateLimiter) setWindowHeaders(c *gin.Context, count int64, window time.Time, limit int) {
	suffix := rl.getWindowHeaderSuffix()

	limitHeader := "X-RateLimit-Limit" + suffix
	remainingHeader := "X-RateLimit-Remaining" + suffix
	resetHeader := "X-RateLimit-Reset" + suffix

	// X-RateLimit-Limit-<Window>: Maximum requests per window
	c.Header(limitHeader, strconv.Itoa(limit))

	// X-RateLimit-Remaining-<Window>: Remaining requests in current window
	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	c.Header(remainingHeader, strconv.Itoa(remaining))

	// X-RateLimit-Reset-<Window>: Time when the window resets
	windowDuration := rl.getWindowDuration()
	reset := window.Add(windowDuration)
	c.Header(resetHeader, strconv.FormatInt(reset.Unix(), 10))
}

func (rl *rateLimiter) getWindowHeaderSuffix() string {
	switch rl.window {
	case TimeWindowMinute:
		return "-Minute"
	case TimeWindowHour:
		return "-Hour"
	case TimeWindowDay:
		return "-Day"
	default:
		return ""
	}
}

// standardMiddleware implements immediate rejection rate limiting.
func (rl *rateLimiter) standardMiddleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if rl.skipFunc != nil && rl.skipFunc(c) {
				next(c)
				return
			}

			key := rl.getKey(c)

			// Check for zero burst edge case
			rps, burst := rl.getRpsAndBurst(key)
			if burst <= 0 && !(rps <= 0 && burst <= 0) {
				// Zero burst (but not unlimited case) - reject immediately
				if rl.headers {
					c.Header("X-RateLimit-Limit", "0")
					c.Header("X-RateLimit-Remaining", "0")
				}
				if rl.retryAfterHeader {
					c.Header("Retry-After", "1")
				}
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":       "rate limit exceeded",
					"retry_after": 1,
				})
				return
			}

			limiter := rl.getLimiter(key)

			if !limiter.Allow() {
				rl.handleRateLimit(c, limiter)
				return
			}

			if rl.headers {
				rl.setHeaders(c, limiter)
			}

			next(c)
		}
	}
}

// waitMiddleware implements waiting rate limiting that waits for available tokens.
func (rl *rateLimiter) waitMiddleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if rl.skipFunc != nil && rl.skipFunc(c) {
				next(c)
				return
			}

			key := rl.getKey(c)
			limiter := rl.getLimiter(key)

			// Use Wait method to wait for available tokens
			ctx, cancel := context.WithTimeout(c.Request.Context(), rl.waitTimeout)
			defer cancel()

			if err := limiter.Wait(ctx); err != nil {
				// Calculate accurate retry-after using the same method as standard middleware
				var retryAfter int64 = 1 // Default minimum
				reservation := limiter.Reserve()
				if reservation.OK() {
					delay := reservation.Delay()
					reservation.Cancel() // Cancel the reservation

					// Calculate retry-after (round up to next second, minimum 1)
					retryAfter = int64(delay.Seconds())
					if delay.Nanoseconds()%int64(time.Second) > 0 {
						retryAfter++ // Round up to next second
					}
					if retryAfter < 1 {
						retryAfter = 1 // Minimum 1 second
					}
				}

				// Set headers including accurate Retry-After on timeout
				if rl.headers {
					rl.setHeaders(c, limiter)
				}
				if rl.retryAfterHeader {
					c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
				}

				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":       "rate limit exceeded",
					"timeout":     rl.waitTimeout.Seconds(),
					"retry_after": retryAfter,
				})
				return
			}

			if rl.headers {
				rl.setHeaders(c, limiter)
			}

			next(c)
		}
	}
}

// getKey returns the rate limiting key for the given context.
// Uses the configured key function or defaults to IP-based key.
func (rl *rateLimiter) getKey(c *gin.Context) string {
	keyFunc := rl.keyFunc
	if keyFunc == nil {
		keyFunc = defaultKeyFunc
	}
	return keyFunc(c)
}

// getRpsAndBurst returns the current rps and burst values for a given key.
func (rl *rateLimiter) getRpsAndBurst(key string) (int, int) {
	if rl.dynamicLimits != nil {
		return rl.dynamicLimits(key)
	}
	return rl.rps, rl.burst
}

// getWindowLimit returns the current window limit for a given key.
func (rl *rateLimiter) getWindowLimit(key string) int {
	if rl.dynamicWindow != nil {
		return rl.dynamicWindow(key)
	}
	return rl.limit
}

// getLimiter retrieves or creates a rate limiter for the given key.
// Supports both static and dynamic rate limits based on configuration.
func (rl *rateLimiter) getLimiter(key string) *rate.Limiter {
	// Lazy initialization of default store
	if rl.store == nil {
		defaultStoreOnce.Do(func() {
			defaultStore = NewMemoryLimiterStore(5 * time.Minute)
		})
		rl.store = defaultStore
	}

	limiter, exists := rl.store.Get(key)

	// Get current limits (static or dynamic)
	rps, burst := rl.getRpsAndBurst(key)

	// Handle invalid limits
	var limitRate rate.Limit
	if rps <= 0 && burst <= 0 {
		// Both invalid - treat as unlimited
		limitRate = rate.Inf
		burst = 1000000 // Large but reasonable burst for unlimited
	} else {
		// Handle individual invalid values by setting to safe defaults
		if rps <= 0 {
			rps = 1 // Minimum valid rate
		}
		if burst <= 0 {
			burst = 1 // Minimum valid burst (will be handled by middleware)
		}
		limitRate = rate.Limit(rps)
	}

	if !exists {
		// Create new limiter
		limiter = rate.NewLimiter(limitRate, burst)
		rl.store.Set(key, limiter)
	} else if rl.dynamicLimits != nil {
		// Update existing limiter if dynamic limits changed
		if limitRate != limiter.Limit() {
			limiter.SetLimit(limitRate)
		}
		if burst != limiter.Burst() {
			limiter.SetBurst(burst)
		}
	}

	return limiter
}

// handleRateLimit processes a rate-limited request and sends appropriate response.
func (rl *rateLimiter) handleRateLimit(c *gin.Context, limiter *rate.Limiter) {
	// Use Reserve to get accurate wait time without consuming a token
	reservation := limiter.Reserve()
	if !reservation.OK() {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error": "rate limit exceeded",
		})
		return
	}

	delay := reservation.Delay()
	reservation.Cancel() // Cancel the reservation

	// Calculate retry-after once (round up to next second, minimum 1)
	retryAfter := int64(delay.Seconds())
	if delay.Nanoseconds()%int64(time.Second) > 0 {
		retryAfter++ // Round up to next second
	}
	if retryAfter < 1 {
		retryAfter = 1 // Minimum 1 second
	}

	if rl.headers {
		rl.setHeaders(c, limiter) // Set rate limit headers
	}
	if rl.retryAfterHeader {
		c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
	}

	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":       "rate limit exceeded",
		"retry_after": retryAfter,
	})
}

// setHeaders adds X-RateLimit-* headers to the response.
func (rl *rateLimiter) setHeaders(c *gin.Context, limiter *rate.Limiter) {
	// Get actual limits from limiter (handles both static and dynamic limits correctly)
	limitRate := limiter.Limit()
	burst := limiter.Burst()

	// Skip headers for unlimited rate (rate.Inf)
	if limitRate == rate.Inf {
		return
	}

	rps := int(limitRate)

	// X-RateLimit-Limit: Rate limit (requests per second)
	c.Header("X-RateLimit-Limit", strconv.Itoa(rps))

	// X-RateLimit-Remaining: Current available tokens
	remaining := max(int(limiter.Tokens()), 0)
	c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))

	// X-RateLimit-Reset: Time when the token bucket will be fully replenished
	tokensNeeded := burst - remaining
	if tokensNeeded <= 0 {
		// Token bucket is full, reset in next second
		c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
	} else {
		// Calculate time needed to recover the full token bucket
		secondsToRecover := float64(tokensNeeded) / float64(rps)
		reset := time.Now().Add(time.Duration(secondsToRecover * float64(time.Second)))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
	}
}

// ============================================================================
// Options-based API - Unified and Clean Interface
// ============================================================================

// RateOption configures the rate limiter behavior.
// Options provide a flexible way to customize rate limiting without
// exposing complex configuration methods.
type RateOption func(*rateLimiter)

// WithIP configures rate limiting by client IP address.
// Each IP gets its own rate limit bucket.
// Note: This is the default behavior, so this option is typically redundant.
func WithIP() RateOption {
	return func(rl *rateLimiter) {
		rl.keyFunc = defaultKeyFunc
	}
}

// WithUser configures rate limiting by authenticated user ID.
// Falls back to IP-based limiting if no user ID is found.
// Users are identified by 'user_id' in the Gin context (set by auth middleware) or X-User-ID header.
func WithUser() RateOption {
	return func(rl *rateLimiter) {
		rl.keyFunc = func(c *gin.Context) string {
			// Try context first (set by auth middleware)
			if userID, exists := GetUserID(c); exists {
				return "user:" + userID
			}
			// Try X-User-ID header as fallback
			if headerUser := c.GetHeader("X-User-ID"); headerUser != "" {
				return "user:" + headerUser
			}
			// Fallback to IP-based limiting
			return c.ClientIP()
		}
	}
}

// WithPath configures rate limiting by IP and path combination.
// This allows different rate limits for different endpoints per client.
func WithPath() RateOption {
	return func(rl *rateLimiter) {
		rl.keyFunc = func(c *gin.Context) string {
			return fmt.Sprintf("%s:%s", c.ClientIP(), c.Request.URL.Path)
		}
	}
}

// WithStore configures a custom storage backend for rate limiters.
// This allows distributed rate limiting using Redis or other systems.
//
// Resource Management: Custom stores are automatically registered and will be
// cleaned up when CleanupRateLimiters() is called. Manual cleanup is optional
// but can be done by calling store.Close() directly if needed.
//
// Example:
//
//	store := NewMemoryLimiterStore(10 * time.Minute)
//	r.Use(ginx.RateLimit(100, 200, ginx.WithStore(store)))
//	// Automatic cleanup at shutdown: ginx.CleanupRateLimiters()
func WithStore(store RateLimitStore) RateOption {
	return func(rl *rateLimiter) {
		// Ignore nil store - will fall back to default lazy-loaded store
		if store == nil {
			return
		}

		rl.store = store
		// Register custom store for automatic cleanup
		activeStoresMutex.Lock()
		activeStores[store] = struct{}{}
		activeStoresMutex.Unlock()
	}
}

// WithWindowStore configures a custom storage backend for window-based rate limiters.
// This is used for per-minute, per-hour, and per-day rate limiting.
//
// Resource Management: Custom window stores are automatically registered and will be
// cleaned up when CleanupRateLimiters() is called.
//
// Example:
//
//	store := NewMemoryWindowCounterStore(25 * time.Hour)
//	r.Use(ginx.RateLimitPerHour(1000, ginx.WithWindowStore(store)))
//	// Automatic cleanup at shutdown: ginx.CleanupRateLimiters()
func WithWindowStore(store WindowCounterStore) RateOption {
	return func(rl *rateLimiter) {
		// Ignore nil store - will fall back to default lazy-loaded store
		if store == nil {
			return
		}

		rl.windowStore = store
		// Register custom store for automatic cleanup
		activeWindowStoresMutex.Lock()
		activeWindowStores[store] = struct{}{}
		activeWindowStoresMutex.Unlock()
	}
}

// WithKeyFunc configures a custom key generation function.
// The key function determines how requests are grouped for rate limiting.
func WithKeyFunc(keyFunc func(*gin.Context) string) RateOption {
	return func(rl *rateLimiter) {
		rl.keyFunc = keyFunc
	}
}

// WithSkipFunc configures a function to skip rate limiting for certain requests.
// Useful for exempting admin users, health checks, etc.
func WithSkipFunc(skipFunc func(*gin.Context) bool) RateOption {
	return func(rl *rateLimiter) {
		rl.skipFunc = skipFunc
	}
}

// WithoutRateLimitHeaders disables X-RateLimit-* headers in responses.
// By default, X-RateLimit-Limit, X-RateLimit-Remaining, and X-RateLimit-Reset headers are included.
// This does NOT affect Retry-After headers.
func WithoutRateLimitHeaders() RateOption {
	return func(rl *rateLimiter) {
		rl.headers = false
	}
}

// WithoutRetryAfterHeader disables Retry-After header in 429 responses.
// By default, Retry-After header is included in rate-limited responses as recommended by RFC 7231.
// Use this option only if you need to completely disable retry guidance for clients.
func WithoutRetryAfterHeader() RateOption {
	return func(rl *rateLimiter) {
		rl.retryAfterHeader = false
	}
}

// WithWait configures the rate limiter to wait for available tokens instead of
// immediately rejecting requests. If the wait time exceeds the timeout,
// the request is rejected with a 429 status.
func WithWait(timeout time.Duration) RateOption {
	return func(rl *rateLimiter) {
		rl.waitTimeout = timeout
	}
}

// WithDynamicLimits configures dynamic rate limiting where different keys
// can have different limits determined at runtime by the provided function.
// The function receives a key and should return (rps, burst) for that key.
// Note: When using this option, the rps and burst parameters to RateLimit
// are ignored as they will be determined dynamically.
// This option only works with RateLimit (token bucket), not with time-window rate limiting.
func WithDynamicLimits(getLimits func(key string) (rps int, burst int)) RateOption {
	return func(rl *rateLimiter) {
		rl.dynamicLimits = getLimits
	}
}

// WithDynamicWindowLimits configures dynamic time-window rate limiting where different keys
// can have different limits determined at runtime by the provided function.
// The function receives a key and should return the limit for that key.
// Note: When using this option, the limit parameter to RateLimitPerMinute/Hour/Day
// is ignored as it will be determined dynamically.
// This option only works with RateLimitPerMinute/Hour/Day, not with RateLimit (token bucket).
//
// Example:
//
//	r.Use(ginx.RateLimitPerHour(0, // Base limit ignored when using dynamic limits
//	    ginx.WithUser(),
//	    ginx.WithDynamicWindowLimits(func(key string) int {
//	        if strings.Contains(key, "user:premium_") {
//	            return 100000  // Premium: 100k per hour
//	        }
//	        if strings.Contains(key, "user:pro_") {
//	            return 10000   // Pro: 10k per hour
//	        }
//	        return 1000        // Free: 1k per hour
//	    })))
func WithDynamicWindowLimits(getLimit func(key string) int) RateOption {
	return func(rl *rateLimiter) {
		rl.dynamicWindow = getLimit
	}
}

// RateLimit creates a rate limiting middleware with the specified limits and options.
// This is the recommended way to configure rate limiting with maximum flexibility.
//
// Resource Management:
// All stores (both default shared and custom stores) are automatically managed.
// Use CleanupRateLimiters() at application shutdown for comprehensive cleanup.
//
// Parameters:
//   - rps: Maximum requests per second allowed
//   - burst: Maximum burst size (tokens that can be consumed immediately)
//   - opts: Optional configuration functions
//
// Examples:
//
//	// Basic rate limiting (uses shared global store)
//	r.Use(ginx.RateLimit(100, 200))
//
//	// Rate limiting by authenticated user (uses shared store)
//	r.Use(ginx.RateLimit(50, 100, ginx.WithUser()))
//
//	// Custom store (automatically managed)
//	store := ginx.NewMemoryLimiterStore(10 * time.Minute)
//	r.Use(ginx.RateLimit(10, 20, ginx.WithStore(store)))
//	// Cleanup at shutdown: ginx.CleanupRateLimiters()
//
//	// Skip rate limiting for admin users
//	r.Use(ginx.RateLimit(100, 200, ginx.WithSkipFunc(isAdminUser)))
func RateLimit(rps, burst int, opts ...RateOption) Middleware {
	limiter := newRateLimiter(rps, burst)

	// Apply all options
	for _, opt := range opts {
		opt(limiter)
	}

	return limiter.Middleware()
}

// RateLimitPerMinute creates a rate limiting middleware that limits requests per minute.
// Uses a fixed window algorithm (window resets at 0 seconds of each minute).
//
// Parameters:
//   - limit: Maximum requests allowed per minute
//   - opts: Optional configuration functions (WithUser, WithPath, etc.)
//
// Examples:
//
//	// Limit to 60 requests per minute
//	r.Use(ginx.RateLimitPerMinute(60))
//
//	// Per-user limit of 100 requests per minute
//	r.Use(ginx.RateLimitPerMinute(100, ginx.WithUser()))
func RateLimitPerMinute(limit int, opts ...RateOption) Middleware {
	limiter := newWindowRateLimiter(limit, TimeWindowMinute)

	// Apply all options
	for _, opt := range opts {
		opt(limiter)
	}

	return limiter.Middleware()
}

// RateLimitPerHour creates a rate limiting middleware that limits requests per hour.
// Uses a fixed window algorithm (window resets at 0 minutes of each hour).
//
// Parameters:
//   - limit: Maximum requests allowed per hour
//   - opts: Optional configuration functions (WithUser, WithPath, etc.)
//
// Examples:
//
//	// Limit to 1000 requests per hour
//	r.Use(ginx.RateLimitPerHour(1000))
//
//	// Per-user limit of 500 requests per hour
//	r.Use(ginx.RateLimitPerHour(500, ginx.WithUser()))
func RateLimitPerHour(limit int, opts ...RateOption) Middleware {
	limiter := newWindowRateLimiter(limit, TimeWindowHour)

	// Apply all options
	for _, opt := range opts {
		opt(limiter)
	}

	return limiter.Middleware()
}

// RateLimitPerDay creates a rate limiting middleware that limits requests per day.
// Uses a fixed window algorithm (window resets at midnight).
//
// Parameters:
//   - limit: Maximum requests allowed per day (resets at midnight)
//   - opts: Optional configuration functions (WithUser, WithPath, etc.)
//
// Examples:
//
//	// Limit to 10000 requests per day
//	r.Use(ginx.RateLimitPerDay(10000))
//
//	// Per-user limit of 5000 requests per day
//	r.Use(ginx.RateLimitPerDay(5000, ginx.WithUser()))
func RateLimitPerDay(limit int, opts ...RateOption) Middleware {
	limiter := newWindowRateLimiter(limit, TimeWindowDay)

	// Apply all options
	for _, opt := range opts {
		opt(limiter)
	}

	return limiter.Middleware()
}

// CleanupRateLimiters provides comprehensive cleanup of all rate limiter stores.
// It cleans up both token bucket stores and window counter stores, including default
// shared stores and all custom stores created with WithStore() or WithWindowStore().
//
// Usage:
//
//	// At application shutdown
//	defer func() {
//		ginx.CleanupRateLimiters()
//	}()
//
// This function is goroutine-safe and can be called multiple times safely.
func CleanupRateLimiters() {
	// Clean up token bucket stores
	activeStoresMutex.Lock()
	stores := make([]RateLimitStore, 0, len(activeStores))
	for store := range activeStores {
		stores = append(stores, store)
	}
	// Clear the registry immediately to prevent new registrations during cleanup
	activeStores = make(map[RateLimitStore]struct{})
	activeStoresMutex.Unlock()

	// Close all stores outside the lock to avoid deadlock
	for _, store := range stores {
		if store != nil {
			store.Close()
		}
	}

	// Clean up window counter stores
	activeWindowStoresMutex.Lock()
	windowStores := make([]WindowCounterStore, 0, len(activeWindowStores))
	for store := range activeWindowStores {
		windowStores = append(windowStores, store)
	}
	// Clear the registry immediately to prevent new registrations during cleanup
	activeWindowStores = make(map[WindowCounterStore]struct{})
	activeWindowStoresMutex.Unlock()

	// Close all window stores outside the lock to avoid deadlock
	for _, store := range windowStores {
		if store != nil {
			store.Close()
		}
	}

	// Reset default stores (they're already closed through the registry)
	defaultStore = nil
	defaultStoreOnce = sync.Once{}
	defaultWindowStore = nil
	defaultWindowStoreOnce = sync.Once{}
}

// ============================================================================
// Helper Functions
// ============================================================================

// defaultKeyFunc generates rate limiting keys based on client IP address.
var defaultKeyFunc = (*gin.Context).ClientIP
