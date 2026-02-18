package ginx

import (
	"context"
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
		ginx.WithoutRateLimitHeaders()))

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
	TimeWindowMinute                   // Per minute (fixed window)
	TimeWindowHour                     // Per hour (fixed window)
	TimeWindowDay                      // Per day (fixed window)
)

// ============================================================================
// Rate Limiting Middleware
// ============================================================================

// rateLimiter represents a configurable rate limiting middleware.
// It uses the token bucket algorithm via golang.org/x/time/rate for precise rate limiting.
type rateLimiter struct {
	store            RateLimitStore
	windowStore      WindowCounterStore
	createMu         sync.Mutex
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
// The store field is initialized eagerly in the public API functions (RateLimit, etc.) after options are applied.
func newRateLimiter(rps, burst int) *rateLimiter {
	return &rateLimiter{
		rps:              rps,
		burst:            burst,
		limit:            0,                // Not used for token bucket mode
		window:           TimeWindowSecond, // Default to per-second limiting
		keyFunc:          defaultKeyFunc,
		headers:          true,
		retryAfterHeader: true, // Enable Retry-After by default
	}
}

// newWindowRateLimiter creates a new time-window based rate limiter.
// The windowStore field is initialized eagerly in the public API functions (RateLimitPer*, etc.) after options are applied.
func newWindowRateLimiter(limit int, window TimeWindow) *rateLimiter {
	return &rateLimiter{
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

// standardMiddleware implements immediate rejection rate limiting.
func (rl *rateLimiter) standardMiddleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if rl.skipFunc != nil && rl.skipFunc(c) {
				next(c)
				return
			}

			key := rl.getKey(c)
			if rl.rejectZeroBurst(c, key) {
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
			if rl.rejectZeroBurst(c, key) {
				return
			}
			limiter := rl.getLimiter(key)

			// Use Wait method to wait for available tokens
			ctx, cancel := context.WithTimeout(c.Request.Context(), rl.waitTimeout)
			defer cancel()

			if err := limiter.Wait(ctx); err != nil {
				// Reserve+Cancel pattern: same trade-off as handleRateLimit — the
				// Retry-After estimate may be slightly inaccurate under high contention,
				// which is acceptable for an advisory header (see handleRateLimit comment).
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

func (rl *rateLimiter) rejectZeroBurst(c *gin.Context, key string) bool {
	rps, burst := rl.getRpsAndBurst(key)
	if burst > 0 || (rps <= 0 && burst <= 0) {
		return false
	}

	if rl.headers {
		c.Header("X-RateLimit-Limit", strconv.Itoa(rps))
		c.Header("X-RateLimit-Remaining", "0")
	}
	if rl.retryAfterHeader {
		c.Header("Retry-After", "1")
	}

	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":       "rate limit exceeded",
		"retry_after": 1,
	})
	return true
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

// getLimiter retrieves or creates a rate limiter for the given key.
// Supports both static and dynamic rate limits based on configuration.
//
// Locking order: rl.createMu → rl.store.Set (which acquires store's internal lock).
// Custom RateLimitStore implementations must not acquire locks that could deadlock
// with this ordering — see the RateLimitStore interface documentation.
func (rl *rateLimiter) getLimiter(key string) *rate.Limiter {
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
		rl.createMu.Lock()
		defer rl.createMu.Unlock()

		// Double-check after acquiring lock to ensure single initialization per key.
		limiter, exists = rl.store.Get(key)
		if !exists {
			limiter = rate.NewLimiter(limitRate, burst)
			rl.store.Set(key, limiter)
		}
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
	// Reserve+Cancel pattern: we call Reserve() solely to obtain the estimated delay
	// until a token would be available, then immediately Cancel() to avoid consuming
	// a token. Under high contention other goroutines may acquire or return tokens
	// between Reserve and Cancel, making the Retry-After estimate slightly inaccurate
	// (typically by a fraction of a second). This trade-off is acceptable because:
	//   1. Retry-After is inherently advisory (RFC 7231 §7.1.3).
	//   2. The alternative (tracking reservation state) adds significant complexity
	//      for negligible accuracy gain.
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

// defaultKeyFunc generates rate limiting keys based on client IP address.
var defaultKeyFunc = (*gin.Context).ClientIP
