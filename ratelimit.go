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

This package implements token bucket rate limiting using golang.org/x/time/rate
for precise, high-performance rate limiting with minimal overhead.

Key Features:
  - Token bucket algorithm for smooth rate limiting
  - Configurable storage backends (memory included, Redis support via interface)
  - Per-IP, per-user, or custom key-based rate limiting
  - HTTP header support (X-RateLimit-* headers)
  - Dynamic rate limiting with per-key limits
  - Waiting middleware variant for traffic smoothing
  - Thread-safe with automatic cleanup of expired limiters

Basic Usage:

	// Simple rate limiting: 100 requests per second, burst of 200
	r.Use(ginx.RateLimit(100, 200))

	// Per-IP rate limiting
	r.Use(ginx.RateLimitByIP(10, 20))

	// Per-user rate limiting (requires authentication middleware)
	r.Use(ginx.RateLimitByUser(50, 100))

Advanced Usage:

	// Custom configuration
	limiter := ginx.NewRateLimiter(100, 200).
		WithKeyFunc(customKeyFunc).
		WithSkipFunc(skipAdmins).
		WithStore(redisStore)
	r.Use(limiter.Middleware())

	// Dynamic per-user limits
	r.Use(ginx.RateLimitPerUser(func(userID string) (rps, burst int) {
		if isPremium(userID) {
			return 1000, 2000
		}
		return 100, 200
	}))

Thread Safety:

All components are thread-safe and designed for high-concurrency environments.
The memory store includes automatic cleanup to prevent memory leaks.
*/

// ============================================================================
// Rate Limiting - Simplified and High-Performance Design
// ============================================================================

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

// MemoryLimiterStore provides an in-memory implementation of RateLimitStore.
// It includes automatic cleanup of expired limiters to prevent memory leaks.
type MemoryLimiterStore struct {
	mu         sync.RWMutex
	limiters   map[string]*rate.Limiter
	lastAccess map[string]time.Time
	maxIdle    time.Duration

	// Cleanup goroutine control
	ticker *time.Ticker
	done   chan struct{}
}

// NewMemoryLimiterStore creates a new in-memory limiter store with automatic cleanup.
// The maxIdle parameter specifies how long unused limiters are kept before cleanup.
// If maxIdle <= 0, it defaults to 5 minutes.
func NewMemoryLimiterStore(maxIdle time.Duration) *MemoryLimiterStore {
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
	close(s.done)
	s.ticker.Stop()
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

// ============================================================================
// Rate Limiting Middleware
// ============================================================================

// RateLimiter represents a configurable rate limiting middleware.
// It uses the token bucket algorithm via golang.org/x/time/rate for precise rate limiting.
type RateLimiter struct {
	store    RateLimitStore
	rps      int
	burst    int
	keyFunc  func(*gin.Context) string
	skipFunc func(*gin.Context) bool
	headers  bool
}

// NewRateLimiter creates a new rate limiter with the specified requests per second (rps) and burst capacity.
// It uses an in-memory store by default and enables HTTP headers by default.
//
// Parameters:
//   - rps: Maximum requests per second allowed
//   - burst: Maximum burst size (tokens that can be consumed immediately)
func NewRateLimiter(rps, burst int) *RateLimiter {
	return &RateLimiter{
		store:   NewMemoryLimiterStore(0),
		rps:     rps,
		burst:   burst,
		keyFunc: defaultKeyFunc,
		headers: true,
	}
}

// WithStore configures the rate limiter to use a custom storage backend.
// This allows for distributed rate limiting using Redis or other storage systems.
func (rl *RateLimiter) WithStore(store RateLimitStore) *RateLimiter {
	rl.store = store
	return rl
}

// WithKeyFunc configures a custom function to generate rate limiting keys.
// The key function determines how requests are grouped for rate limiting.
// If nil, defaults to using client IP address.
func (rl *RateLimiter) WithKeyFunc(keyFunc func(*gin.Context) string) *RateLimiter {
	rl.keyFunc = keyFunc
	return rl
}

// WithSkipFunc configures a function to determine which requests should skip rate limiting.
// This is useful for exempting certain users, IPs, or request types from rate limits.
func (rl *RateLimiter) WithSkipFunc(skipFunc func(*gin.Context) bool) *RateLimiter {
	rl.skipFunc = skipFunc
	return rl
}

// WithoutHeaders disables the automatic setting of X-RateLimit-* HTTP headers.
// By default, rate limit information is included in response headers.
func (rl *RateLimiter) WithoutHeaders() *RateLimiter {
	rl.headers = false
	return rl
}

// Middleware returns a Gin middleware function that enforces rate limiting.
// The middleware checks the rate limit for each request and either allows it to proceed
// or returns a 429 Too Many Requests response.
func (rl *RateLimiter) Middleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if rl.skipFunc != nil && rl.skipFunc(c) {
				next(c)
				return
			}

			keyFunc := rl.keyFunc
			if keyFunc == nil {
				keyFunc = defaultKeyFunc
			}
			key := keyFunc(c)
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

// getLimiter retrieves or creates a rate limiter for the given key.
func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	limiter, exists := rl.store.Get(key)
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(rl.rps), rl.burst)
		rl.store.Set(key, limiter)
	}
	return limiter
}

// handleRateLimit processes a rate-limited request and sends appropriate response.
func (rl *RateLimiter) handleRateLimit(c *gin.Context, limiter *rate.Limiter) {
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

	if rl.headers {
		c.Header("Retry-After", strconv.FormatInt(int64(delay.Seconds())+1, 10))
	}

	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":       "rate limit exceeded",
		"retry_after": int64(delay.Seconds()) + 1,
	})
}

// setHeaders adds X-RateLimit-* headers to the response.
func (rl *RateLimiter) setHeaders(c *gin.Context, limiter *rate.Limiter) {
	// X-RateLimit-Limit: Rate limit (requests per second)
	c.Header("X-RateLimit-Limit", strconv.Itoa(rl.rps))

	// X-RateLimit-Remaining: Current available tokens
	// Use the current token count from the bucket, which is the most accurate
	remaining := int(limiter.Tokens())
	c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))

	// X-RateLimit-Reset: Time when the token bucket will be fully replenished
	// Calculate when the bucket will be completely refilled
	tokensNeeded := rl.burst - remaining
	if tokensNeeded <= 0 {
		// Token bucket is full, next calculation starts in the next second
		c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
	} else {
		// Calculate time needed to recover the full token bucket
		secondsToRecover := float64(tokensNeeded) / float64(rl.rps)
		reset := time.Now().Add(time.Duration(secondsToRecover * float64(time.Second)))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
	}
}

// Close shuts down the rate limiter and cleans up resources.
// It should be called when the rate limiter is no longer needed.
func (rl *RateLimiter) Close() error {
	return rl.store.Close()
}

// ============================================================================
// Convenience Functions - Super Simple API
// ============================================================================

// RateLimit creates a simple rate limiting middleware with the specified rps and burst.
// This is a convenience function for basic rate limiting needs.
//
// Example:
//
//	r.Use(ginx.RateLimit(100, 200)) // 100 rps with burst of 200
func RateLimit(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).Middleware()
}

// RateLimitByIP creates a rate limiter that limits requests by client IP address.
// Each IP address gets its own rate limit bucket.
//
// Example:
//
//	r.Use(ginx.RateLimitByIP(10, 20)) // 10 rps per IP with burst of 20
func RateLimitByIP(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).
		WithKeyFunc(KeyByIP()).
		Middleware()
}

// RateLimitByUser creates a rate limiter that limits requests by authenticated user.
// Users are identified by the 'user_id' value in the Gin context.
// Falls back to IP-based limiting if no user_id is found.
//
// Example:
//
//	r.Use(ginx.RateLimitByUser(50, 100)) // 50 rps per user with burst of 100
func RateLimitByUser(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).
		WithKeyFunc(KeyByUserID()).
		Middleware()
}

// RateLimitByPath creates a rate limiter that limits requests by IP and path combination.
// This allows different rate limits for different endpoints per client.
//
// Example:
//
//	r.Use(ginx.RateLimitByPath(5, 10)) // 5 rps per IP per path with burst of 10
func RateLimitByPath(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).
		WithKeyFunc(KeyByPath()).
		Middleware()
}

// ============================================================================
// Advanced Features - With Wait Support
// ============================================================================

// RateLimitWithWait creates a rate limiting middleware that waits for available tokens
// instead of immediately rejecting requests. If the wait time exceeds the timeout,
// the request is rejected with a 429 status.
//
// This is useful for smoothing out traffic spikes while still providing rate limiting.
//
// Parameters:
//   - rps: Maximum requests per second
//   - burst: Burst capacity
//   - timeout: Maximum time to wait for tokens
//
// Example:
//
//	r.Use(ginx.RateLimitWithWait(10, 20, 5*time.Second))
func RateLimitWithWait(rps, burst int, timeout time.Duration) Middleware {
	limiter := NewRateLimiter(rps, burst)

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if limiter.skipFunc != nil && limiter.skipFunc(c) {
				next(c)
				return
			}

			keyFunc := limiter.keyFunc
			if keyFunc == nil {
				keyFunc = defaultKeyFunc
			}
			key := keyFunc(c)
			rateLimiter := limiter.getLimiter(key)

			// Use Wait method to wait for available tokens
			ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
			defer cancel()

			if err := rateLimiter.Wait(ctx); err != nil {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":   "rate limit exceeded",
					"timeout": timeout.Seconds(),
				})
				return
			}

			next(c)
		}
	}
}

// ============================================================================
// Conditions - Rate Limiting
// ============================================================================

// IsRateLimited returns a condition function that checks if a request would be rate limited
// without actually consuming tokens. This is useful for conditional middleware chains.
//
// Parameters:
//   - store: The rate limiter store to check against
//   - rps: Requests per second limit
//   - burst: Burst capacity
//   - keyFunc: Function to generate rate limiting keys (nil for default IP-based)
//
// Example:
//
//	chain := ginx.NewChain().
//	  When(ginx.IsRateLimited(store, 10, 20, nil), ginx.ThrottleMiddleware())
func IsRateLimited(store RateLimitStore, rps, burst int, keyFunc func(*gin.Context) string) Condition {
	if keyFunc == nil {
		keyFunc = defaultKeyFunc
	}

	return func(c *gin.Context) bool {
		key := keyFunc(c)
		limiter, exists := store.Get(key)
		if !exists {
			// Non-existent limiter means no requests have been made yet, so not rate limited
			return false
		}

		// Use Reserve to check without consuming tokens
		reservation := limiter.Reserve()
		if !reservation.OK() {
			return true
		}

		delay := reservation.Delay()
		reservation.Cancel() // Cancel the reservation

		return delay > 0
	}
}

// ============================================================================
// Per-User Dynamic Rate Limiting
// ============================================================================

// DynamicRateLimiter provides per-key dynamic rate limiting where different keys
// can have different rate limits determined at runtime by a callback function.
// This is useful for implementing tiered rate limiting based on user type, subscription level, etc.
type DynamicRateLimiter struct {
	store      RateLimitStore
	getLimiter func(key string) (rps int, burst int)
	keyFunc    func(*gin.Context) string
	skipFunc   func(*gin.Context) bool
	headers    bool
}

// NewDynamicRateLimiter creates a new dynamic rate limiter with a callback function
// that determines rate limits per key. By default, it uses user ID-based keys.
//
// The getLimiter function receives a key and should return (rps, burst) for that key.
//
// Example:
//
//	limiter := ginx.NewDynamicRateLimiter(func(userID string) (int, int) {
//	  if isPremiumUser(userID) {
//	    return 1000, 2000 // Premium users get higher limits
//	  }
//	  return 100, 200    // Regular users
//	})
func NewDynamicRateLimiter(getLimiter func(key string) (rps int, burst int)) *DynamicRateLimiter {
	return &DynamicRateLimiter{
		store:      NewMemoryLimiterStore(0),
		getLimiter: getLimiter,
		keyFunc:    KeyByUserID(),
		headers:    true,
	}
}

// Middleware returns a Gin middleware function for dynamic rate limiting.
// Each request's rate limit is determined by calling the getLimiter function.
func (dl *DynamicRateLimiter) Middleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if dl.skipFunc != nil && dl.skipFunc(c) {
				next(c)
				return
			}

			key := dl.keyFunc(c)
			limiter := dl.getRateLimiter(key)

			if !limiter.Allow() {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "rate limit exceeded",
				})
				return
			}

			next(c)
		}
	}
}

// getRateLimiter retrieves or creates a dynamic rate limiter for the given key.
func (dl *DynamicRateLimiter) getRateLimiter(key string) *rate.Limiter {
	limiter, exists := dl.store.Get(key)
	if !exists {
		rps, burst := dl.getLimiter(key)
		limiter = rate.NewLimiter(rate.Limit(rps), burst)
		dl.store.Set(key, limiter)
	}
	return limiter
}

// RateLimitPerUser creates a dynamic rate limiting middleware where each user
// can have different rate limits based on the provided function.
//
// Example:
//
//	middleware := ginx.RateLimitPerUser(func(userID string) (int, int) {
//	  plan := getUserPlan(userID)
//	  switch plan {
//	  case "premium":
//	    return 1000, 2000
//	  case "basic":
//	    return 100, 200
//	  default:
//	    return 10, 20
//	  }
//	})
func RateLimitPerUser(getLimiter func(userID string) (rps int, burst int)) Middleware {
	return NewDynamicRateLimiter(getLimiter).Middleware()
}

// ============================================================================
// Helper Functions
// ============================================================================

// defaultKeyFunc generates rate limiting keys based on client IP address.
func defaultKeyFunc(c *gin.Context) string {
	return c.ClientIP()
}

// KeyByIP returns a key function that uses the client IP address for rate limiting.
// This is the default behavior and treats each IP address separately.
func KeyByIP() func(*gin.Context) string {
	return func(c *gin.Context) string {
		return c.ClientIP()
	}
}

// KeyByUserID returns a key function that uses the authenticated user ID for rate limiting.
// It looks for 'user_id' in the Gin context and falls back to IP if not found.
// User IDs are prefixed with 'user:' to avoid conflicts with IP addresses.
func KeyByUserID() func(*gin.Context) string {
	return func(c *gin.Context) string {
		if userID, exists := c.Get("user_id"); exists {
			if id, ok := userID.(string); ok {
				return "user:" + id
			}
		}
		return c.ClientIP() // Fallback to IP
	}
}

// KeyByPath returns a key function that combines client IP and request path.
// This allows different rate limits for different endpoints per client.
// Format: "IP:path" (e.g., "192.168.1.1:/api/users")
func KeyByPath() func(*gin.Context) string {
	return func(c *gin.Context) string {
		return fmt.Sprintf("%s:%s", c.ClientIP(), c.Request.URL.Path)
	}
}
