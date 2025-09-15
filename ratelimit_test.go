package ginx

import (
	"net/http"
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
		c.Set("user_id", "user123")

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
