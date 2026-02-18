package ginx

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// windowMiddleware implements time-window based rate limiting (minute/hour/day)
func (rl *rateLimiter) windowMiddleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if rl.skipFunc != nil && rl.skipFunc(c) {
				next(c)
				return
			}

			// Prefix key with window type to prevent counter collisions
			// when multiple window-based limiters share the same store
			rawKey := rl.getKey(c)
			key := fmt.Sprintf("%s:w%d", rawKey, rl.window)

			// Get current window
			window := rl.getCurrentWindow(time.Now())

			// Get limit (static or dynamic, using raw key for user callbacks)
			limit := rl.getWindowLimit(rawKey)

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

// getWindowLimit returns the current window limit for a given key.
func (rl *rateLimiter) getWindowLimit(key string) int {
	if rl.dynamicWindow != nil {
		return rl.dynamicWindow(key)
	}
	return rl.limit
}
