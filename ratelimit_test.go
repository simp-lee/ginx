package ginx

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestMemoryLimiterStore(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should store and retrieve limiters", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		limiter := rate.NewLimiter(10, 20)
		key := "test-key"

		// Initially should not exist
		_, exists := store.Get(key)
		assert.False(t, exists)

		// Store the limiter
		store.Set(key, limiter)

		// Should retrieve the same limiter
		retrieved, exists := store.Get(key)
		assert.True(t, exists)
		assert.Same(t, limiter, retrieved)
	})

	t.Run("should delete limiters", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		limiter := rate.NewLimiter(10, 20)
		key := "test-key"

		store.Set(key, limiter)
		_, exists := store.Get(key)
		assert.True(t, exists)

		store.Delete(key)
		_, exists = store.Get(key)
		assert.False(t, exists)
	})

	t.Run("should clear all limiters", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		limiter1 := rate.NewLimiter(10, 20)
		limiter2 := rate.NewLimiter(5, 10)

		store.Set("key1", limiter1)
		store.Set("key2", limiter2)

		// Both should exist
		_, exists := store.Get("key1")
		assert.True(t, exists)
		_, exists = store.Get("key2")
		assert.True(t, exists)

		// Clear all
		store.Clear()

		// Both should be gone
		_, exists = store.Get("key1")
		assert.False(t, exists)
		_, exists = store.Get("key2")
		assert.False(t, exists)
	})

	t.Run("should cleanup expired limiters", func(t *testing.T) {
		store := NewMemoryLimiterStore(50 * time.Millisecond)
		defer store.Close()

		limiter := rate.NewLimiter(10, 20)
		store.Set("test-key", limiter)

		// Should exist initially
		_, exists := store.Get("test-key")
		assert.True(t, exists)

		// Wait for cleanup
		time.Sleep(100 * time.Millisecond)

		// Should be cleaned up
		_, exists = store.Get("test-key")
		assert.False(t, exists)
	})
}

func TestRateLimiterBasic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should allow requests within burst", func(t *testing.T) {
		// Use a dedicated store for this test to avoid interference
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store)) // 10 rps, burst 5

		// First 5 requests should pass (burst capacity)
		for i := 0; i < 5; i++ {
			c, w := TestContext("GET", "/test", nil)

			handler := middleware(func(c *gin.Context) {
				c.JSON(200, gin.H{"success": true})
			})

			handler(c)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("should return 429 when burst exceeded", func(t *testing.T) {
		// Use a dedicated store for this test to avoid interference
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 2, WithStore(store)) // 1 rps, burst 2

		// Use up burst
		for i := 0; i < 2; i++ {
			c, w := TestContext("GET", "/test", nil)
			handler := middleware(func(c *gin.Context) {
				c.JSON(200, gin.H{"success": true})
			})
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// Next request should be rate limited
		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("should use custom key function", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithKeyFunc(func(c *gin.Context) string {
			return "fixed-key"
		}))

		// First request
		c1, w1 := TestContext("GET", "/test", nil)
		c1.Request.RemoteAddr = "192.0.2.1:1234"
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request from different IP should still be limited
		c2, w2 := TestContext("GET", "/test", nil)
		c2.Request.RemoteAddr = "192.0.2.2:1234"
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	})

	t.Run("should skip when skip function returns true", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithSkipFunc(func(c *gin.Context) bool {
			return c.GetHeader("X-Skip") == "true"
		}))

		// Use up limit
		c1, w1 := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Should be limited normally
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// Should skip with header
		c3, w3 := TestContext("GET", "/test", map[string]string{
			"X-Skip": "true",
		})
		handler(c3)
		assert.Equal(t, http.StatusOK, w3.Code)
	})

	t.Run("should disable headers when configured", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store), WithoutRateLimitHeaders())

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)

		// Headers should not be set
		limit := w.Header().Get("X-RateLimit-Limit")
		assert.Empty(t, limit)
	})

	t.Run("should set correct rate limit headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store)) // 10 rps, burst 5

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)

		// Check headers are set
		limit := w.Header().Get("X-RateLimit-Limit")
		assert.Equal(t, "10", limit)

		remaining := w.Header().Get("X-RateLimit-Remaining")
		assert.NotEmpty(t, remaining)

		reset := w.Header().Get("X-RateLimit-Reset")
		assert.NotEmpty(t, reset)

		// Remaining should be between 0 and 5 (burst capacity)
		remainingInt, err := strconv.Atoi(remaining)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, remainingInt, 0)
		assert.LessOrEqual(t, remainingInt, 5)
	})
}

func TestConvenienceFunctions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("RateLimit should work", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store))

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("RateLimit with WithIP should work", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store), WithIP())

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("RateLimit with WithUser should work", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store), WithUser())

		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestDynamicRateLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	getLimiter := func(key string) (rps int, burst int) {
		if key == "user:premium" {
			return 100, 200
		}
		return 10, 20
	}

	t.Run("should apply different limits for different users", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(0, 0, WithStore(store), WithUser(), WithDynamicLimits(getLimiter))

		// Premium user should have higher limits
		c1, w1 := TestContext("GET", "/test", nil)
		c1.Set("user_id", "premium")

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Regular user
		c2, w2 := TestContext("GET", "/test", nil)
		c2.Set("user_id", "regular")

		handler(c2)
		assert.Equal(t, http.StatusOK, w2.Code)
	})

	t.Run("should handle zero burst correctly with dynamic limits", func(t *testing.T) {
		// Test case for the bug: dynamic limits with zero burst but non-zero static config
		getDynamicLimits := func(key string) (rps int, burst int) {
			if key == "user:zero-burst" {
				return 10, 0 // Dynamic: 10 rps, 0 burst (should be rejected immediately)
			}
			if key == "user:unlimited" {
				return 0, 0 // Dynamic: unlimited (should be allowed)
			}
			return 10, 20 // Default limits
		}

		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		// Use non-zero static config (100, 200) but dynamic limits may be (10, 0) or (0, 0)
		middleware := RateLimit(100, 200, WithStore(store), WithUser(), WithDynamicLimits(getDynamicLimits))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Test 1: User with dynamic zero burst should be rejected immediately
		c1, w1 := TestContext("GET", "/test", nil)
		SetUserID(c1, "zero-burst")
		handler(c1)
		assert.Equal(t, http.StatusTooManyRequests, w1.Code, "Zero burst user should be rate limited immediately")

		// Test 2: User with dynamic unlimited (0, 0) should be allowed despite static config
		c2, w2 := TestContext("GET", "/test", nil)
		SetUserID(c2, "unlimited")
		handler(c2)
		assert.Equal(t, http.StatusOK, w2.Code, "Unlimited user should be allowed")

		// Test 3: Regular user with normal limits should work
		c3, w3 := TestContext("GET", "/test", nil)
		SetUserID(c3, "regular")
		handler(c3)
		assert.Equal(t, http.StatusOK, w3.Code, "Regular user should be allowed")
	})
}

func TestRateLimiterEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should handle zero rps gracefully", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(0, 1, WithStore(store))
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		// With 0 rps, should still allow burst requests
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should handle zero burst gracefully", func(t *testing.T) {
		middleware := RateLimit(10, 0)
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		// With 0 burst, should immediately rate limit
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("should handle very large values", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1000000, 2000000, WithStore(store))
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should handle nil skip function", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store), WithSkipFunc(nil))
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should handle nil key function", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store), WithKeyFunc(nil))
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRateLimiterConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should be thread safe", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(100, 200, WithStore(store))
		const numGoroutines = 10
		const requestsPerGoroutine = 10

		results := make(chan int, numGoroutines*requestsPerGoroutine)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				for j := 0; j < requestsPerGoroutine; j++ {
					c, w := TestContext("GET", "/test", nil)
					handler := middleware(func(c *gin.Context) {
						c.JSON(200, gin.H{"success": true})
					})
					handler(c)
					results <- w.Code
				}
			}()
		}

		successCount := 0
		rateLimitedCount := 0

		for i := 0; i < numGoroutines*requestsPerGoroutine; i++ {
			code := <-results
			switch code {
			case http.StatusOK:
				successCount++
			case http.StatusTooManyRequests:
				rateLimitedCount++
			}
		}

		// Should have some successful requests
		assert.Greater(t, successCount, 0)
		// Total should equal expected
		assert.Equal(t, numGoroutines*requestsPerGoroutine, successCount+rateLimitedCount)
	})
}

func TestRateLimiterHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should set correct headers when remaining is 0", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store)) // Very restrictive

		// First request - should succeed
		c1, w1 := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request - should be rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// Check retry-after header is set
		retryAfter := w2.Header().Get("Retry-After")
		assert.NotEmpty(t, retryAfter)
	})

	t.Run("should have correct header values", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 5, WithStore(store))
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)

		// Validate header formats
		limit := w.Header().Get("X-RateLimit-Limit")
		assert.Equal(t, "10", limit)

		remaining := w.Header().Get("X-RateLimit-Remaining")
		remainingInt, err := strconv.Atoi(remaining)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, remainingInt, 0)

		reset := w.Header().Get("X-RateLimit-Reset")
		resetInt, err := strconv.ParseInt(reset, 10, 64)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, resetInt, time.Now().Unix())
	})
}

func TestRateLimitWithWait(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should timeout when wait time exceeds limit", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithWait(10*time.Millisecond))

		// Use up the bucket first
		c1, w1 := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request should timeout
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	})

	t.Run("should work with sufficient timeout", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(10, 1, WithStore(store), WithWait(5*time.Second))

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestMemoryLimiterStoreEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should handle zero maxIdle", func(t *testing.T) {
		store := NewMemoryLimiterStore(0) // Should default to 5 minutes
		defer store.Close()

		limiter := rate.NewLimiter(10, 20)
		store.Set("test", limiter)

		retrieved, exists := store.Get("test")
		assert.True(t, exists)
		assert.Same(t, limiter, retrieved)
	})

	t.Run("should handle negative maxIdle", func(t *testing.T) {
		store := NewMemoryLimiterStore(-time.Minute) // Should default to 5 minutes
		defer store.Close()

		limiter := rate.NewLimiter(10, 20)
		store.Set("test", limiter)

		retrieved, exists := store.Get("test")
		assert.True(t, exists)
		assert.Same(t, limiter, retrieved)
	})

	t.Run("should update access time on Get", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Hour)
		defer store.Close()

		limiter := rate.NewLimiter(10, 20)
		store.Set("test", limiter)

		// Wait a bit then access
		time.Sleep(10 * time.Millisecond)
		_, exists := store.Get("test")
		assert.True(t, exists)

		// Access time should be updated (harder to test precisely)
		// But we can at least ensure it still exists
		_, exists = store.Get("test")
		assert.True(t, exists)
	})
}

// Benchmark tests
func BenchmarkRateLimiter(b *testing.B) {
	gin.SetMode(gin.TestMode)
	middleware := RateLimit(1000, 2000)
	handler := middleware(func(c *gin.Context) {
		c.Status(200)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := TestContext("GET", "/test", nil)
		handler(c)
	}
}

// TestDynamicRateLimitUpdates tests dynamic limiter updates and header consistency
func TestDynamicRateLimitUpdates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Track which limits are returned for specific keys
	limitsMap := map[string][2]int{
		"user:test": {10, 20}, // Initial limits
	}

	getLimits := func(key string) (rps int, burst int) {
		limits := limitsMap[key]
		return limits[0], limits[1]
	}

	t.Run("should update existing limiter when dynamic limits change", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithUser(), WithDynamicLimits(getLimits)) // Use non-zero base to avoid unlimited mode

		// First request creates limiter with initial limits
		c1, w1 := TestContext("GET", "/test", nil)
		SetUserID(c1, "test")

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)
		assert.Equal(t, "10", w1.Header().Get("X-RateLimit-Limit"))

		// Change the dynamic limits
		limitsMap["user:test"] = [2]int{50, 100}

		// Second request should update the limiter and reflect new limits in headers
		c2, w2 := TestContext("GET", "/test", nil)
		SetUserID(c2, "test")

		handler(c2)
		assert.Equal(t, http.StatusOK, w2.Code)
		assert.Equal(t, "50", w2.Header().Get("X-RateLimit-Limit")) // Updated limit
	})

	t.Run("should maintain header consistency with actual limiter behavior", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		// Set very low limits to easily trigger rate limiting
		limitsMap["user:header"] = [2]int{1, 1}

		middleware := RateLimit(1, 1, WithStore(store), WithUser(), WithDynamicLimits(getLimits)) // Use non-zero base

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// First request should succeed
		c1, w1 := TestContext("GET", "/test", nil)
		SetUserID(c1, "header")
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)
		assert.Equal(t, "1", w1.Header().Get("X-RateLimit-Limit"))

		// Second request should be rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		SetUserID(c2, "header")
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
		assert.Equal(t, "1", w2.Header().Get("X-RateLimit-Limit"))
		assert.Equal(t, "0", w2.Header().Get("X-RateLimit-Remaining"))
	})
}

// TestWaitMiddlewareRetryAfterHeaders tests that wait middleware sets Retry-After headers on timeout
func TestWaitMiddlewareRetryAfterHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should set Retry-After header on wait timeout", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		// Very restrictive limits to force timeout
		middleware := RateLimit(1, 1, WithStore(store), WithWait(50*time.Millisecond))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// First request consumes the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request should timeout and have Retry-After header
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		retryAfter := w2.Header().Get("Retry-After")
		assert.NotEmpty(t, retryAfter)

		// Should be at least 1 second (our minimum)
		retrySeconds, err := strconv.Atoi(retryAfter)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, retrySeconds, 1)
	})

	t.Run("should set rate limit headers even on timeout", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		middleware := RateLimit(5, 2, WithStore(store), WithWait(10*time.Millisecond))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume tokens
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusOK, w2.Code)

		// This should timeout
		c3, w3 := TestContext("GET", "/test", nil)
		handler(c3)
		assert.Equal(t, http.StatusTooManyRequests, w3.Code)
		assert.Equal(t, "5", w3.Header().Get("X-RateLimit-Limit"))
		assert.Equal(t, "0", w3.Header().Get("X-RateLimit-Remaining"))
		assert.NotEmpty(t, w3.Header().Get("Retry-After"))
	})
}

// TestHeaderDataSourceConsistency tests that headers reflect actual limiter state
func TestHeaderDataSourceConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should use limiter values not configuration values for headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		// Static configuration values
		middleware := RateLimit(100, 200, WithStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "100", w.Header().Get("X-RateLimit-Limit"))
		assert.Equal(t, "199", w.Header().Get("X-RateLimit-Remaining")) // burst - 1 token used
	})

	t.Run("should skip headers for unlimited rate", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		// Both rps and burst are 0 - should be unlimited
		middleware := RateLimit(0, 0, WithStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		// Headers should be empty for unlimited rate
		assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
		assert.Empty(t, w.Header().Get("X-RateLimit-Remaining"))
		assert.Empty(t, w.Header().Get("X-RateLimit-Reset"))
	})
}

// TestInvalidConfigurationCombinations tests various invalid rps/burst combinations
func TestInvalidConfigurationCombinations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should handle zero rps with positive burst", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		middleware := RateLimit(0, 5, WithStore(store)) // 0 rps, positive burst

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		// Should work (rps gets set to minimum 1)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "1", w.Header().Get("X-RateLimit-Limit")) // Minimum valid rps
	})

	t.Run("should handle negative values", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		middleware := RateLimit(-10, -5, WithStore(store)) // Negative values

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		// Should be treated as unlimited (both negative)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Header().Get("X-RateLimit-Limit")) // No headers for unlimited
	})
}

// TestRetryAfterConsistency tests that Retry-After is calculated consistently
func TestRetryAfterConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should have consistent Retry-After in header and JSON", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		middleware := RateLimit(1, 1, WithStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Get rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		retryAfterHeader := w2.Header().Get("Retry-After")
		assert.NotEmpty(t, retryAfterHeader)

		// Parse JSON response to check retry_after field
		var response map[string]interface{}
		err := json.Unmarshal(w2.Body.Bytes(), &response)
		assert.NoError(t, err)

		retryAfterJSON, exists := response["retry_after"]
		assert.True(t, exists)

		// Convert both to strings for comparison (JSON numbers might be float64)
		headerSeconds, _ := strconv.Atoi(retryAfterHeader)
		jsonSeconds := int(retryAfterJSON.(float64))

		// Should be the same value
		assert.Equal(t, headerSeconds, jsonSeconds)
		assert.GreaterOrEqual(t, headerSeconds, 1) // At least 1 second
	})
}

// TestWithoutHeadersRetryAfterBehavior tests WithoutHeaders and WithoutRetryAfter behavior (deprecated naming)
func TestWithoutHeadersRetryAfterBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("WithoutHeaders should disable X-RateLimit-* but keep Retry-After headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithoutRateLimitHeaders()) // Very restrictive to trigger rate limiting

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// First request - consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request - should be rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// X-RateLimit-* headers should be empty
		assert.Empty(t, w2.Header().Get("X-RateLimit-Limit"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Remaining"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Reset"))
		// But Retry-After should still be present (RFC recommendation)
		assert.NotEmpty(t, w2.Header().Get("Retry-After"), "Retry-After should be kept even with WithoutHeaders")
	})

	t.Run("WithoutHeaders should keep Retry-After in Wait mode too", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithWait(10*time.Millisecond), WithoutRateLimitHeaders())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume the token first
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// This should timeout and be rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// X-RateLimit-* headers should be empty
		assert.Empty(t, w2.Header().Get("X-RateLimit-Limit"))
		// But Retry-After should still be present
		assert.NotEmpty(t, w2.Header().Get("Retry-After"), "Wait mode Retry-After should be kept even with WithoutHeaders")
	})

	t.Run("WithoutRetryAfter should disable only Retry-After headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithoutRetryAfterHeader()) // Keep X-RateLimit-* but disable Retry-After

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// First request - consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request - should be rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// X-RateLimit-* headers should be present
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Limit"))
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Remaining"))
		// But Retry-After should be disabled
		assert.Empty(t, w2.Header().Get("Retry-After"), "Retry-After should be disabled with WithoutRetryAfter")
	})

	t.Run("WithoutHeaders and WithoutRetryAfter should disable all headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithoutRateLimitHeaders(), WithoutRetryAfterHeader())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// First request - consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request - should be rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// All headers should be empty
		assert.Empty(t, w2.Header().Get("X-RateLimit-Limit"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Remaining"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Reset"))
		assert.Empty(t, w2.Header().Get("Retry-After"))
	})
}

// TestRateLimitHeaderConfiguration tests the new naming for header configuration
func TestRateLimitHeaderConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("WithoutRateLimitHeaders should disable only X-RateLimit-* headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithoutRateLimitHeaders())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Get rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// X-RateLimit-* headers should be disabled
		assert.Empty(t, w2.Header().Get("X-RateLimit-Limit"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Remaining"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Reset"))
		// But Retry-After should still be present
		assert.NotEmpty(t, w2.Header().Get("Retry-After"))
	})

	t.Run("WithoutRetryAfterHeader should disable only Retry-After header", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store), WithoutRetryAfterHeader())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Get rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// X-RateLimit-* headers should be present
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Limit"))
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Remaining"))
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Reset"))
		// But Retry-After should be disabled
		assert.Empty(t, w2.Header().Get("Retry-After"))
	})

	t.Run("Combine both options to disable all headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store),
			WithoutRateLimitHeaders(),
			WithoutRetryAfterHeader(),
		)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Get rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// All headers should be disabled
		assert.Empty(t, w2.Header().Get("X-RateLimit-Limit"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Remaining"))
		assert.Empty(t, w2.Header().Get("X-RateLimit-Reset"))
		assert.Empty(t, w2.Header().Get("Retry-After"))
	})

	t.Run("Default behavior includes all headers", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()
		middleware := RateLimit(1, 1, WithStore(store)) // No options = all headers enabled

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume the token
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Get rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// All headers should be present by default
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Limit"))
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Remaining"))
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Reset"))
		assert.NotEmpty(t, w2.Header().Get("Retry-After"))
	})
}

// TestWaitVsStandardRetryAfterConsistency tests consistency between wait and standard mode Retry-After calculation
func TestWaitVsStandardRetryAfterConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("wait mode should use accurate delay calculation not timeout value", func(t *testing.T) {
		store1 := NewMemoryLimiterStore(time.Minute)
		defer store1.Close()
		store2 := NewMemoryLimiterStore(time.Minute)
		defer store2.Close()

		// Both use same rate limits but wait timeout is very short to force timeout
		standardMiddleware := RateLimit(2, 1, WithStore(store1))                            // 2 rps, burst 1
		waitMiddleware := RateLimit(2, 1, WithStore(store2), WithWait(10*time.Millisecond)) // Same limits, very short timeout

		standardHandler := standardMiddleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		waitHandler := waitMiddleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Consume tokens in both
		c1, w1 := TestContext("GET", "/test", nil)
		standardHandler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		c2, w2 := TestContext("GET", "/test", nil)
		waitHandler(c2)
		assert.Equal(t, http.StatusOK, w2.Code)

		// Get rate limited responses
		c3, w3 := TestContext("GET", "/test", nil)
		standardHandler(c3)
		assert.Equal(t, http.StatusTooManyRequests, w3.Code)

		c4, w4 := TestContext("GET", "/test", nil)
		waitHandler(c4)
		assert.Equal(t, http.StatusTooManyRequests, w4.Code)

		standardRetryAfter := w3.Header().Get("Retry-After")
		waitRetryAfter := w4.Header().Get("Retry-After")

		assert.NotEmpty(t, standardRetryAfter)
		assert.NotEmpty(t, waitRetryAfter)

		// Both should use accurate delay calculation, not timeout value
		standardSeconds, _ := strconv.Atoi(standardRetryAfter)
		waitSeconds, _ := strconv.Atoi(waitRetryAfter)

		// After fix, both should be similar (based on actual token availability, not timeout)
		// Both should use actual delay calculation (~0.5s for 2rps, rounded up to 1)
		assert.Equal(t, 1, standardSeconds, "Standard mode uses actual delay calculation")
		assert.Equal(t, 1, waitSeconds, "Wait mode should also use actual delay calculation, not timeout value")
	})
}

func BenchmarkMemoryStore(b *testing.B) {
	store := NewMemoryLimiterStore(time.Hour)
	defer store.Close()

	limiter := rate.NewLimiter(1000, 2000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "key" + string(rune(i%100))
			if i%10 == 0 {
				store.Set(key, limiter)
			} else {
				store.Get(key)
			}
			i++
		}
	})
}
