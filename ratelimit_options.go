package ginx

import (
	"fmt"
	"net/textproto"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

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
// Users are identified only by 'user_id' in the Gin context (set by auth middleware).
func WithUser() RateOption {
	return func(rl *rateLimiter) {
		rl.keyFunc = func(c *gin.Context) string {
			if userID, exists := GetUserID(c); exists {
				return "user:" + userID
			}
			// Fallback to IP-based limiting when user context is unavailable
			return c.ClientIP()
		}
	}
}

// WithTrustedUserHeader configures rate limiting by authenticated user ID with explicit
// trusted-header fallback for deployments behind a trusted gateway.
//
// Security: only use this option when the header is set by trusted infrastructure
// (e.g., API gateway / auth proxy) and cannot be controlled by external clients.
func WithTrustedUserHeader(headerName string) RateOption {
	canonicalHeader := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(headerName))

	return func(rl *rateLimiter) {
		if canonicalHeader == "" {
			WithUser()(rl)
			return
		}

		rl.keyFunc = func(c *gin.Context) string {
			if userID, exists := GetUserID(c); exists {
				return "user:" + userID
			}
			if headerUser := strings.TrimSpace(c.GetHeader(canonicalHeader)); headerUser != "" {
				return "user:" + headerUser
			}
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
		// Ignore nil store - will fall back to default store
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
		// Ignore nil store - will fall back to default store
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
