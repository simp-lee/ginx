package ginx

import (
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
		limiter := NewRateLimiter(10, 5) // 10 rps, burst 5
		defer limiter.Close()

		middleware := limiter.Middleware()

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
		limiter := NewRateLimiter(1, 2) // 1 rps, burst 2
		defer limiter.Close()

		middleware := limiter.Middleware()

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
		limiter := NewRateLimiter(1, 1).
			WithKeyFunc(func(c *gin.Context) string {
				return "fixed-key"
			})
		defer limiter.Close()

		middleware := limiter.Middleware()

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
		limiter := NewRateLimiter(1, 1).
			WithSkipFunc(func(c *gin.Context) bool {
				return c.GetHeader("X-Skip") == "true"
			})
		defer limiter.Close()

		middleware := limiter.Middleware()

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
		limiter := NewRateLimiter(10, 5).WithoutHeaders()
		defer limiter.Close()

		middleware := limiter.Middleware()

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
		limiter := NewRateLimiter(10, 5) // 10 rps, burst 5
		defer limiter.Close()

		middleware := limiter.Middleware()

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
		middleware := RateLimit(10, 5)

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("RateLimitByIP should work", func(t *testing.T) {
		middleware := RateLimitByIP(10, 5)

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("RateLimitByUser should work", func(t *testing.T) {
		middleware := RateLimitByUser(10, 5)

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
		middleware := RateLimitPerUser(getLimiter)

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
}

func TestIsRateLimited(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return false when not limited", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		condition := IsRateLimited(store, 10, 5, nil)
		c, _ := TestContext("GET", "/test", nil)

		result := condition(c)
		assert.False(t, result)
	})

	t.Run("should return true when limited", func(t *testing.T) {
		store := NewMemoryLimiterStore(time.Minute)
		defer store.Close()

		// Pre-populate with exhausted limiter
		limiter := rate.NewLimiter(1, 1)
		limiter.Allow() // Use up the burst
		store.Set("192.0.2.1", limiter)

		condition := IsRateLimited(store, 1, 1, nil)
		c, _ := TestContext("GET", "/test", nil)

		result := condition(c)
		assert.True(t, result)
	})
}

func TestRateLimiterEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should handle zero rps gracefully", func(t *testing.T) {
		limiter := NewRateLimiter(0, 1)
		defer limiter.Close()

		middleware := limiter.Middleware()
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		// With 0 rps, should still allow burst requests
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should handle zero burst gracefully", func(t *testing.T) {
		limiter := NewRateLimiter(10, 0)
		defer limiter.Close()

		middleware := limiter.Middleware()
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		// With 0 burst, should immediately rate limit
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("should handle very large values", func(t *testing.T) {
		limiter := NewRateLimiter(1000000, 2000000)
		defer limiter.Close()

		middleware := limiter.Middleware()
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should handle nil skip function", func(t *testing.T) {
		limiter := NewRateLimiter(10, 5).WithSkipFunc(nil)
		defer limiter.Close()

		middleware := limiter.Middleware()
		c, w := TestContext("GET", "/test", nil)

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should handle nil key function", func(t *testing.T) {
		limiter := NewRateLimiter(10, 5).WithKeyFunc(nil)
		defer limiter.Close()

		middleware := limiter.Middleware()
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
		limiter := NewRateLimiter(100, 200)
		defer limiter.Close()

		middleware := limiter.Middleware()
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
		limiter := NewRateLimiter(1, 1) // Very restrictive
		defer limiter.Close()

		middleware := limiter.Middleware()

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
		limiter := NewRateLimiter(10, 5)
		defer limiter.Close()

		middleware := limiter.Middleware()
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
		middleware := RateLimitWithWait(1, 1, 10*time.Millisecond)

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
		middleware := RateLimitWithWait(10, 1, 5*time.Second)

		c, w := TestContext("GET", "/test", nil)
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestKeyFunctions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("KeyByUserID should fallback to IP when no user_id", func(t *testing.T) {
		keyFunc := KeyByUserID()
		c, _ := TestContext("GET", "/test", nil)
		c.Request.RemoteAddr = "192.168.1.1:8080"

		key := keyFunc(c)
		assert.Equal(t, "192.168.1.1", key) // Should fallback to IP
	})

	t.Run("KeyByUserID should use user_id when available", func(t *testing.T) {
		keyFunc := KeyByUserID()
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		key := keyFunc(c)
		assert.Equal(t, "user:user123", key)
	})

	t.Run("KeyByUserID should fallback when user_id is not string", func(t *testing.T) {
		keyFunc := KeyByUserID()
		c, _ := TestContext("GET", "/test", nil)
		c.Set("user_id", 123) // Not a string
		c.Request.RemoteAddr = "192.168.1.1:8080"

		key := keyFunc(c)
		assert.Equal(t, "192.168.1.1", key)
	})

	t.Run("KeyByPath should include path in key", func(t *testing.T) {
		keyFunc := KeyByPath()
		c, _ := TestContext("GET", "/api/users", nil)
		c.Request.RemoteAddr = "192.168.1.1:8080"

		key := keyFunc(c)
		assert.Equal(t, "192.168.1.1:/api/users", key)
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
	limiter := NewRateLimiter(1000, 2000)
	defer limiter.Close()

	middleware := limiter.Middleware()
	handler := middleware(func(c *gin.Context) {
		c.Status(200)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := TestContext("GET", "/test", nil)
		handler(c)
	}
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
