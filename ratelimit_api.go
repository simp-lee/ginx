package ginx

import (
	"sync"
	"time"
)

// ============================================================================
// Public API
// ============================================================================

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

	// Fail fast on misconfiguration: burst <= 0 with positive rps means every
	// request will be rejected (token bucket has zero capacity). This is almost
	// certainly a configuration error.  Dynamic limits are exempt because the
	// effective burst is determined at request time, not at construction time.
	if rps > 0 && burst <= 0 && limiter.dynamicLimits == nil {
		panic("ginx: RateLimit called with burst <= 0 and rps > 0; this rejects all requests (use burst >= 1)")
	}

	// Eagerly initialize default store if none provided via options.
	// Protected by defaultStoreMu to prevent races with CleanupRateLimiters.
	if limiter.store == nil {
		defaultStoreMu.Lock()
		defaultStoreOnce.Do(func() {
			defaultStore = NewMemoryLimiterStore(5 * time.Minute)
		})
		limiter.store = defaultStore
		defaultStoreMu.Unlock()
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

	// Eagerly initialize default window store if none provided via options.
	// Protected by defaultWindowStoreMu to prevent races with CleanupRateLimiters.
	if limiter.windowStore == nil {
		defaultWindowStoreMu.Lock()
		defaultWindowStoreOnce.Do(func() {
			defaultWindowStore = NewMemoryWindowCounterStore(25 * time.Hour)
		})
		limiter.windowStore = defaultWindowStore
		defaultWindowStoreMu.Unlock()
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

	// Eagerly initialize default window store if none provided via options.
	// Protected by defaultWindowStoreMu to prevent races with CleanupRateLimiters.
	if limiter.windowStore == nil {
		defaultWindowStoreMu.Lock()
		defaultWindowStoreOnce.Do(func() {
			defaultWindowStore = NewMemoryWindowCounterStore(25 * time.Hour)
		})
		limiter.windowStore = defaultWindowStore
		defaultWindowStoreMu.Unlock()
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

	// Eagerly initialize default window store if none provided via options.
	// Protected by defaultWindowStoreMu to prevent races with CleanupRateLimiters.
	if limiter.windowStore == nil {
		defaultWindowStoreMu.Lock()
		defaultWindowStoreOnce.Do(func() {
			defaultWindowStore = NewMemoryWindowCounterStore(25 * time.Hour)
		})
		limiter.windowStore = defaultWindowStore
		defaultWindowStoreMu.Unlock()
	}

	return limiter.Middleware()
}

// CleanupRateLimiters provides comprehensive cleanup of all rate limiter stores.
// It cleans up both token bucket stores and window counter stores, including default
// shared stores and all custom stores created with WithStore() or WithWindowStore().
//
// IMPORTANT: This function must only be called after the HTTP server has fully stopped
// accepting and processing requests. Calling it while requests are still being handled
// will result in undefined behavior.
//
// Usage:
//
//	// At application shutdown (after server.Shutdown())
//	ginx.CleanupRateLimiters()
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

	// Reset default stores so they can be re-initialized if needed (e.g., in tests).
	// Protected by mutexes to prevent races with concurrent RateLimit/RateLimitPer* calls.
	defaultStoreMu.Lock()
	defaultStore = nil
	defaultStoreOnce = sync.Once{}
	defaultStoreMu.Unlock()

	defaultWindowStoreMu.Lock()
	defaultWindowStore = nil
	defaultWindowStoreOnce = sync.Once{}
	defaultWindowStoreMu.Unlock()
}
