package ginx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func windowHeader(base string, window TimeWindow) string {
	switch window {
	case TimeWindowMinute:
		return base + "-Minute"
	case TimeWindowHour:
		return base + "-Hour"
	case TimeWindowDay:
		return base + "-Day"
	default:
		return base
	}
}

func TestMemoryWindowCounterStore(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should store and increment counters", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Minute)
		defer store.Close()

		window := time.Now().Truncate(time.Minute)
		key := "test-key"

		// Initially should be 0
		count, err := store.Get(key, window)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), count)

		// Increment
		newCount, err := store.Increment(key, window)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), newCount)

		// Get should return incremented value
		count, err = store.Get(key, window)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)

		// Increment again
		newCount, err = store.Increment(key, window)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), newCount)
	})

	t.Run("should handle different windows separately", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Minute)
		defer store.Close()

		now := time.Now()
		window1 := now.Truncate(time.Minute)
		window2 := now.Add(time.Minute).Truncate(time.Minute)
		key := "test-key"

		// Increment in window1
		count1, _ := store.Increment(key, window1)
		assert.Equal(t, int64(1), count1)

		// Increment in window2
		count2, _ := store.Increment(key, window2)
		assert.Equal(t, int64(1), count2)

		// Both windows should maintain their own counts
		c1, _ := store.Get(key, window1)
		c2, _ := store.Get(key, window2)
		assert.Equal(t, int64(1), c1)
		assert.Equal(t, int64(1), c2)
	})

	t.Run("should clear all counters", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Minute)
		defer store.Close()

		window := time.Now().Truncate(time.Minute)
		store.Increment("key1", window)
		store.Increment("key2", window)

		store.Clear()

		count1, _ := store.Get("key1", window)
		count2, _ := store.Get("key2", window)
		assert.Equal(t, int64(0), count1)
		assert.Equal(t, int64(0), count2)
	})

	t.Run("should cleanup expired counters", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(100 * time.Millisecond)
		defer store.Close()

		window := time.Now().Truncate(time.Minute)
		store.Increment("test-key", window)

		// Should exist initially
		count, _ := store.Get("test-key", window)
		assert.Equal(t, int64(1), count)

		// Wait for cleanup (generous margin for Windows timer resolution)
		time.Sleep(300 * time.Millisecond)

		// Should be cleaned up
		count, _ = store.Get("test-key", window)
		assert.Equal(t, int64(0), count)
	})

	t.Run("should not panic with tiny positive maxIdle", func(t *testing.T) {
		assert.NotPanics(t, func() {
			store := NewMemoryWindowCounterStore(time.Nanosecond)
			defer store.Close()

			window := time.Now().Truncate(time.Minute)
			count, err := store.Increment("test-key", window)
			assert.NoError(t, err)
			assert.Equal(t, int64(1), count)
		})
	})
}

func TestRateLimitPerMinute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should allow requests within limit", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Hour)
		defer store.Close()
		middleware := RateLimitPerMinute(5, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// First 5 requests should pass
		for i := 0; i < 5; i++ {
			c, w := TestContext("GET", "/test", nil)
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i+1)
		}
	})

	t.Run("should enforce limit under concurrent load", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Minute)
		defer store.Close()

		middleware := RateLimitPerMinute(1, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"success": true})
		})

		const workers = 10
		var wg sync.WaitGroup
		wg.Add(workers)

		var success atomic.Int32
		var limited atomic.Int32

		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				c, w := TestContext("GET", "/test", nil)
				handler(c)
				switch w.Code {
				case http.StatusOK:
					success.Add(1)
				case http.StatusTooManyRequests:
					limited.Add(1)
				}
			}()
		}

		wg.Wait()

		assert.Equal(t, int32(1), success.Load(), "only one request should succeed within the window")
		assert.Equal(t, int32(workers-1), limited.Load(), "all other concurrent requests should be limited")
	})

	t.Run("should return 429 when limit exceeded", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Hour)
		defer store.Close()
		middleware := RateLimitPerMinute(3, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Use up the limit
		for i := 0; i < 3; i++ {
			c, w := TestContext("GET", "/test", nil)
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// Next request should be rate limited
		c, w := TestContext("GET", "/test", nil)
		handler(c)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)

		// Check headers
		limitHeader := windowHeader("X-RateLimit-Limit", TimeWindowMinute)
		remainingHeader := windowHeader("X-RateLimit-Remaining", TimeWindowMinute)
		resetHeader := windowHeader("X-RateLimit-Reset", TimeWindowMinute)

		assert.Equal(t, "3", w.Header().Get(limitHeader))
		assert.Equal(t, "0", w.Header().Get(remainingHeader))
		assert.NotEmpty(t, w.Header().Get(resetHeader))
		assert.NotEmpty(t, w.Header().Get("Retry-After"))
	})

	t.Run("should work with WithUser", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Hour)
		defer store.Close()
		middleware := RateLimitPerMinute(2, WithWindowStore(store), WithUser())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// User1 makes 2 requests
		for i := 0; i < 2; i++ {
			c, w := TestContext("GET", "/test", nil)
			SetUserID(c, "user1")
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// User1's 3rd request should be limited
		c1, w1 := TestContext("GET", "/test", nil)
		SetUserID(c1, "user1")
		handler(c1)
		assert.Equal(t, http.StatusTooManyRequests, w1.Code)

		// User2 should still be able to make requests
		c2, w2 := TestContext("GET", "/test", nil)
		SetUserID(c2, "user2")
		handler(c2)
		assert.Equal(t, http.StatusOK, w2.Code)
	})

	t.Run("should set correct headers", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Hour)
		defer store.Close()
		middleware := RateLimitPerMinute(10, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		limitHeader := windowHeader("X-RateLimit-Limit", TimeWindowMinute)
		remainingHeader := windowHeader("X-RateLimit-Remaining", TimeWindowMinute)
		resetHeader := windowHeader("X-RateLimit-Reset", TimeWindowMinute)

		assert.Equal(t, "10", w.Header().Get(limitHeader))
		assert.Equal(t, "9", w.Header().Get(remainingHeader))

		reset := w.Header().Get(resetHeader)
		assert.NotEmpty(t, reset)
		resetTime, _ := strconv.ParseInt(reset, 10, 64)
		assert.Greater(t, resetTime, time.Now().Unix())
	})
}

func TestRateLimitPerHour(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should allow requests within hourly limit", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(25 * time.Hour)
		defer store.Close()
		middleware := RateLimitPerHour(100, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// First 100 requests should pass
		for i := 0; i < 100; i++ {
			c, w := TestContext("GET", "/test", nil)
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i+1)
		}

		// 101st request should be limited
		c, w := TestContext("GET", "/test", nil)
		handler(c)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("should set correct headers for hourly limit", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(25 * time.Hour)
		defer store.Close()
		middleware := RateLimitPerHour(1000, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		limitHeader := windowHeader("X-RateLimit-Limit", TimeWindowHour)
		remainingHeader := windowHeader("X-RateLimit-Remaining", TimeWindowHour)
		assert.Equal(t, "1000", w.Header().Get(limitHeader))
		assert.Equal(t, "999", w.Header().Get(remainingHeader))
	})

	t.Run("should work with WithPath", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(25 * time.Hour)
		defer store.Close()
		middleware := RateLimitPerHour(5, WithWindowStore(store), WithPath())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Make 5 requests to /api/v1
		for i := 0; i < 5; i++ {
			c, w := TestContext("GET", "/api/v1", nil)
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// 6th request to /api/v1 should be limited
		c1, w1 := TestContext("GET", "/api/v1", nil)
		handler(c1)
		assert.Equal(t, http.StatusTooManyRequests, w1.Code)

		// But request to /api/v2 should succeed (different path)
		c2, w2 := TestContext("GET", "/api/v2", nil)
		handler(c2)
		assert.Equal(t, http.StatusOK, w2.Code)
	})
}

func TestRateLimitPerDay(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should allow requests within daily limit", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(48 * time.Hour)
		defer store.Close()
		middleware := RateLimitPerDay(1000, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Make 1000 requests
		for i := 0; i < 1000; i++ {
			c, w := TestContext("GET", "/test", nil)
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i+1)
		}

		// 1001st request should be limited
		c, w := TestContext("GET", "/test", nil)
		handler(c)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("should set correct headers for daily limit", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(48 * time.Hour)
		defer store.Close()
		middleware := RateLimitPerDay(5000, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		limitHeader := windowHeader("X-RateLimit-Limit", TimeWindowDay)
		remainingHeader := windowHeader("X-RateLimit-Remaining", TimeWindowDay)
		resetHeader := windowHeader("X-RateLimit-Reset", TimeWindowDay)

		assert.Equal(t, "5000", w.Header().Get(limitHeader))
		assert.Equal(t, "4999", w.Header().Get(remainingHeader))

		reset := w.Header().Get(resetHeader)
		assert.NotEmpty(t, reset)
	})

	t.Run("should skip when skip function returns true", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(48 * time.Hour)
		defer store.Close()
		middleware := RateLimitPerDay(2, WithWindowStore(store), WithSkipFunc(func(c *gin.Context) bool {
			return c.GetHeader("X-Admin") == "true"
		}))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Use up the limit
		for i := 0; i < 2; i++ {
			c, w := TestContext("GET", "/test", nil)
			handler(c)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// Should be limited
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusTooManyRequests, w1.Code)

		// But admin should skip limit
		c2, w2 := TestContext("GET", "/test", map[string]string{
			"X-Admin": "true",
		})
		handler(c2)
		assert.Equal(t, http.StatusOK, w2.Code)
	})
}

func TestWindowRateLimitHeaderControl(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should disable rate limit headers when configured", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Hour)
		defer store.Close()
		middleware := RateLimitPerMinute(5, WithWindowStore(store), WithoutRateLimitHeaders())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		c, w := TestContext("GET", "/test", nil)
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		limitHeader := windowHeader("X-RateLimit-Limit", TimeWindowMinute)
		remainingHeader := windowHeader("X-RateLimit-Remaining", TimeWindowMinute)
		resetHeader := windowHeader("X-RateLimit-Reset", TimeWindowMinute)

		assert.Empty(t, w.Header().Get(limitHeader))
		assert.Empty(t, w.Header().Get(remainingHeader))
		assert.Empty(t, w.Header().Get(resetHeader))
	})

	t.Run("should disable retry-after header when configured", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Hour)
		defer store.Close()
		middleware := RateLimitPerMinute(1, WithWindowStore(store), WithoutRetryAfterHeader())

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		// Use up the limit
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Get rate limited
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// Rate limit headers should be present
		limitHeader := windowHeader("X-RateLimit-Limit", TimeWindowMinute)
		remainingHeader := windowHeader("X-RateLimit-Remaining", TimeWindowMinute)
		assert.NotEmpty(t, w2.Header().Get(limitHeader))
		assert.NotEmpty(t, w2.Header().Get(remainingHeader))

		// But Retry-After should be disabled
		assert.Empty(t, w2.Header().Get("Retry-After"))
	})
}

func TestTimeWindowCalculation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should calculate minute window correctly", func(t *testing.T) {
		rl := newWindowRateLimiter(10, TimeWindowMinute)

		now := time.Date(2025, 11, 2, 14, 35, 42, 0, time.UTC)
		window := rl.getCurrentWindow(now)
		expected := time.Date(2025, 11, 2, 14, 35, 0, 0, time.UTC)

		assert.Equal(t, expected, window)
		assert.Equal(t, time.Minute, rl.getWindowDuration())
	})

	t.Run("should calculate hour window correctly", func(t *testing.T) {
		rl := newWindowRateLimiter(100, TimeWindowHour)

		now := time.Date(2025, 11, 2, 14, 35, 42, 0, time.UTC)
		window := rl.getCurrentWindow(now)
		expected := time.Date(2025, 11, 2, 14, 0, 0, 0, time.UTC)

		assert.Equal(t, expected, window)
		assert.Equal(t, time.Hour, rl.getWindowDuration())
	})

	t.Run("should calculate day window correctly", func(t *testing.T) {
		rl := newWindowRateLimiter(1000, TimeWindowDay)

		now := time.Date(2025, 11, 2, 14, 35, 42, 0, time.UTC)
		window := rl.getCurrentWindow(now)
		expected := time.Date(2025, 11, 2, 0, 0, 0, 0, time.UTC)

		assert.Equal(t, expected, window)
		assert.Equal(t, 24*time.Hour, rl.getWindowDuration())
	})
}

func TestCleanupWithWindowStores(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should cleanup both token bucket and window stores", func(t *testing.T) {
		SetupRateLimitTest(t)

		// Create custom stores
		tokenStore := NewMemoryLimiterStore(time.Minute)
		windowStore := NewMemoryWindowCounterStore(time.Hour)

		// Use them in middlewares
		_ = RateLimit(10, 20, WithStore(tokenStore))
		_ = RateLimitPerMinute(60, WithWindowStore(windowStore))

		// Stores should be closed (check by trying to use them)
		// After close, operations should still work but data should be cleared
	})
}

func BenchmarkWindowRateLimit(b *testing.B) {
	gin.SetMode(gin.TestMode)
	middleware := RateLimitPerMinute(10000)
	handler := middleware(func(c *gin.Context) {
		c.Status(200)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := TestContext("GET", "/test", nil)
		handler(c)
	}
}

func BenchmarkWindowStore(b *testing.B) {
	store := NewMemoryWindowCounterStore(time.Hour)
	defer store.Close()

	window := time.Now().Truncate(time.Minute)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "key" + strconv.Itoa(i%100)
			if i%10 == 0 {
				store.Increment(key, window)
			} else {
				store.Get(key, window)
			}
			i++
		}
	})
}

func TestDynamicWindowRateLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should_apply_different_limits_for_different_users_per_minute", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(1 * time.Hour)
		defer store.Close()

		getDynamicLimit := func(key string) int {
			if strings.Contains(key, "user:premium") {
				return 10 // Premium users: 10 per minute
			}
			if strings.Contains(key, "user:pro") {
				return 5 // Pro users: 5 per minute
			}
			return 2 // Free users: 2 per minute
		}

		middleware := RateLimitPerMinute(0, // Base limit ignored
			WithWindowStore(store),
			WithUser(),
			WithDynamicWindowLimits(getDynamicLimit))

		handler := middleware(func(c *gin.Context) {
			c.String(http.StatusOK, "OK")
		})

		// Test premium user (10 per minute)
		for i := 0; i < 10; i++ {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			SetUserID(c, "premium_user")

			handler(c)
			assert.Equal(t, http.StatusOK, w.Code, "Premium user request %d should succeed", i+1)
		}

		// 11th request should fail for premium user
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		SetUserID(c, "premium_user")
		handler(c)
		assert.Equal(t, http.StatusTooManyRequests, w.Code, "Premium user 11th request should be rate limited")

		// Test pro user (5 per minute)
		for i := 0; i < 5; i++ {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			SetUserID(c, "pro_user")

			handler(c)
			assert.Equal(t, http.StatusOK, w.Code, "Pro user request %d should succeed", i+1)
		}

		// 6th request should fail for pro user
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		SetUserID(c, "pro_user")
		handler(c)
		assert.Equal(t, http.StatusTooManyRequests, w.Code, "Pro user 6th request should be rate limited")

		// Test free user (2 per minute)
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			SetUserID(c, "free_user")

			handler(c)
			assert.Equal(t, http.StatusOK, w.Code, "Free user request %d should succeed", i+1)
		}

		// 3rd request should fail for free user
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		SetUserID(c, "free_user")
		handler(c)
		assert.Equal(t, http.StatusTooManyRequests, w.Code, "Free user 3rd request should be rate limited")
	})

	t.Run("should_set_correct_headers_with_dynamic_limits", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(1 * time.Hour)
		defer store.Close()

		getDynamicLimit := func(key string) int {
			if strings.Contains(key, "user:vip") {
				return 100
			}
			return 10
		}

		middleware := RateLimitPerMinute(0,
			WithWindowStore(store),
			WithUser(),
			WithDynamicWindowLimits(getDynamicLimit))

		handler := middleware(func(c *gin.Context) {
			c.String(http.StatusOK, "OK")
		})

		// Test VIP user headers
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		SetUserID(c, "vip_user")
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "100", w.Header().Get("X-RateLimit-Limit-Minute"), "VIP user should have limit 100")
		assert.Equal(t, "99", w.Header().Get("X-RateLimit-Remaining-Minute"), "VIP user should have 99 remaining")

		// Test regular user headers
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		SetUserID(c, "regular_user")
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "10", w.Header().Get("X-RateLimit-Limit-Minute"), "Regular user should have limit 10")
		assert.Equal(t, "9", w.Header().Get("X-RateLimit-Remaining-Minute"), "Regular user should have 9 remaining")
	})

	t.Run("should_work_with_RateLimitPerHour", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(25 * time.Hour)
		defer store.Close()

		getDynamicLimit := func(key string) int {
			if strings.Contains(key, "user:enterprise") {
				return 100000
			}
			return 1000
		}

		middleware := RateLimitPerHour(0,
			WithWindowStore(store),
			WithUser(),
			WithDynamicWindowLimits(getDynamicLimit))

		handler := middleware(func(c *gin.Context) {
			c.String(http.StatusOK, "OK")
		})

		// Test enterprise user
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		SetUserID(c, "enterprise_user")
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "100000", w.Header().Get("X-RateLimit-Limit-Hour"))
		assert.Equal(t, "99999", w.Header().Get("X-RateLimit-Remaining-Hour"))
	})

	t.Run("should_work_with_RateLimitPerDay", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(25 * time.Hour)
		defer store.Close()

		getDynamicLimit := func(key string) int {
			if strings.Contains(key, "user:unlimited") {
				return 1000000
			}
			return 10000
		}

		middleware := RateLimitPerDay(0,
			WithWindowStore(store),
			WithUser(),
			WithDynamicWindowLimits(getDynamicLimit))

		handler := middleware(func(c *gin.Context) {
			c.String(http.StatusOK, "OK")
		})

		// Test unlimited user
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		SetUserID(c, "unlimited_user")
		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "1000000", w.Header().Get("X-RateLimit-Limit-Day"))
		assert.Equal(t, "999999", w.Header().Get("X-RateLimit-Remaining-Day"))
	})
}

func TestWindowRateLimitCustomResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetupRateLimitTest(t)

	customBody := gin.H{
		"code":    429,
		"message": "rate limit exceeded",
		"data":    nil,
	}

	t.Run("window: custom response replaces default body", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Minute)
		defer store.Close()

		middleware := RateLimitPerMinute(1, WithWindowStore(store), WithRateLimitResponse(customBody))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"ok": true})
		})

		// First request succeeds
		c1, w1 := TestContext("GET", "/test", nil)
		handler(c1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request should be rate limited with custom body
		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		var body map[string]any
		err := json.Unmarshal(w2.Body.Bytes(), &body)
		assert.NoError(t, err)
		assert.Equal(t, float64(429), body["code"])
		assert.Equal(t, "rate limit exceeded", body["message"])
		assert.Nil(t, body["data"])
		// Should NOT contain default fields
		assert.Empty(t, body["error"])
		assert.Empty(t, body["retry_after"])

		// Headers should still be set
		assert.NotEmpty(t, w2.Header().Get("Retry-After"))
		assert.NotEmpty(t, w2.Header().Get("X-RateLimit-Limit-Minute"))
	})

	t.Run("window: default response when option not set", func(t *testing.T) {
		store := NewMemoryWindowCounterStore(time.Minute)
		defer store.Close()

		middleware := RateLimitPerMinute(1, WithWindowStore(store))

		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"ok": true})
		})

		c1, _ := TestContext("GET", "/test", nil)
		handler(c1)

		c2, w2 := TestContext("GET", "/test", nil)
		handler(c2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		var body map[string]any
		err := json.Unmarshal(w2.Body.Bytes(), &body)
		assert.NoError(t, err)
		assert.Equal(t, "rate limit exceeded", body["error"])
		assert.NotNil(t, body["retry_after"])
	})
}
