package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestCombinedRateLimiting tests the combination of RPS rate limiting and time window rate limiting
func TestCombinedRateLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("RPS_and_PerMinute_Combined", func(t *testing.T) {
		SetupRateLimitTest(t)

		r := gin.New()

		// Combined rate limiting: 2 requests per second (burst 3) + 5 requests per minute
		r.Use(NewChain().
			Use(RateLimit(2, 3)).       // RPS: 2/s, burst: 3
			Use(RateLimitPerMinute(5)). // 5 requests per minute
			Build())

		r.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Test 1: First 5 requests should pass (not exceeding any limit)
		for i := 1; i <= 5; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i)
			time.Sleep(600 * time.Millisecond) // Avoid exceeding RPS limit
		}

		// Test 2: The 6th request should be blocked by per-minute limit (even if not exceeding RPS limit)
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code, "The 6th request should be blocked by per-minute limit")
	})

	t.Run("RPS_Burst_Blocks_Before_Window", func(t *testing.T) {
		SetupRateLimitTest(t)

		r := gin.New()

		// Combined rate limiting: 1 request per second (burst 2) + 100 requests per minute
		r.Use(NewChain().
			Use(RateLimit(1, 2)).         // Strict RPS
			Use(RateLimitPerMinute(100)). // Relaxed per-minute limit
			Build())

		r.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Test: Send 3 requests quickly, the 3rd should be blocked by RPS limit (not window limit)
		for i := 1; i <= 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "First 2 requests should succeed (burst allowed)")
		}

		// The 3rd request sent immediately should be blocked by RPS limit
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code, "The 3rd request should be blocked by RPS limit")
	})

	t.Run("Three_Layer_Protection", func(t *testing.T) {
		SetupRateLimitTest(t)

		r := gin.New()

		// Three-layer rate limiting: RPS + per-hour + per-day
		r.Use(NewChain().
			Use(RateLimit(10, 20)).     // 10 requests per second
			Use(RateLimitPerHour(3)).   // 3 requests per hour (for testing)
			Use(RateLimitPerDay(1000)). // 1000 requests per day
			Build())

		r.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Test: First 3 requests pass, the 4th is blocked by per-hour limit
		for i := 1; i <= 3; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "First 3 requests should succeed")
			time.Sleep(150 * time.Millisecond) // Avoid RPS limit
		}

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code, "The 4th request should be blocked by per-hour limit")
	})

	t.Run("Headers_From_Combined_Limiters", func(t *testing.T) {
		SetupRateLimitTest(t)

		r := gin.New()

		// Combined rate limiting and check response headers
		r.Use(NewChain().
			Use(RateLimit(10, 20)).
			Use(RateLimitPerMinute(100)).
			Build())

		r.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response headers: RPS uses generic names, time window rate limiting uses suffixed names
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"))
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit-Minute"))
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining-Minute"))
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset-Minute"))
	})

	t.Run("Different_Keys_Independent", func(t *testing.T) {
		SetupRateLimitTest(t)

		r := gin.New()
		r.Use(func(c *gin.Context) {
			if userID := c.GetHeader("X-Auth-User"); userID != "" {
				SetUserID(c, userID)
			}
			c.Next()
		})

		// Use WithUser option, different users have independent counters
		r.Use(NewChain().
			Use(RateLimit(5, 5, WithUser())).
			Use(RateLimitPerMinute(2, WithUser())). // Only 2 requests per minute for easy testing
			Build())

		r.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// User 1 sends 2 requests
		for i := 1; i <= 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-Auth-User", "user1")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "User 1's request %d should succeed", i)
			time.Sleep(300 * time.Millisecond)
		}

		// User 2 can also send 2 requests (independent counter)
		for i := 1; i <= 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-Auth-User", "user2")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "User 2's request %d should succeed (independent counter)", i)
			time.Sleep(300 * time.Millisecond)
		}

		// User 1's 3rd request should be blocked (exceeding 2 requests per minute limit)
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Auth-User", "user1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Code, "User 1's 3rd request should be blocked")
	})
}

// BenchmarkCombinedRateLimiting tests the performance of combined rate limiting
func BenchmarkCombinedRateLimiting(b *testing.B) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Combine multiple rate limiters
	r.Use(NewChain().
		Use(RateLimit(1000, 2000)).
		Use(RateLimitPerMinute(10000)).
		Use(RateLimitPerHour(100000)).
		Build())

	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}
