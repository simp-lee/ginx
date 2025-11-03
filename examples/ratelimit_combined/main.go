package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
)

func main() {
	r := gin.Default()

	// ============================================================================
	// Example 1: Combined RPS and Per-Minute Rate Limiting
	// ============================================================================
	// This endpoint has two layers of protection:
	// 1. Maximum 10 requests per second (burst of 20)
	// 2. Maximum 100 requests per minute
	example1 := r.Group("/api/example1")
	example1.Use(ginx.NewChain().
		Use(ginx.RateLimit(10, 20)).       // 10 rps, burst 20
		Use(ginx.RateLimitPerMinute(100)). // 100 per minute
		Build())
	example1.GET("/data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Success!",
			"limits":  "10 rps (burst 20) AND 100/min",
		})
	})

	// ============================================================================
	// Example 2: Three-Layer Protection (RPS + Hourly + Daily)
	// ============================================================================
	// Complete multi-layer rate limiting:
	// 1. 5 requests per second (prevent instant spikes)
	// 2. 1000 requests per hour (prevent sustained abuse)
	// 3. 10000 requests per day (total quota limit)
	example2 := r.Group("/api/example2")
	example2.Use(ginx.NewChain().
		Use(ginx.RateLimit(5, 10)).       // 5 rps, burst 10
		Use(ginx.RateLimitPerHour(1000)). // 1000 per hour
		Use(ginx.RateLimitPerDay(10000)). // 10000 per day
		Build())
	example2.GET("/premium", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Three-layer protection",
			"limits": map[string]string{
				"instant": "5 rps (burst 10)",
				"hourly":  "1000/hour",
				"daily":   "10000/day",
			},
		})
	})

	// ============================================================================
	// Example 3: User Tier-Based Combined Rate Limiting
	// ============================================================================
	// Free tier: Strict rate limits
	freeGroup := r.Group("/api/free")
	freeGroup.Use(ginx.NewChain().
		Use(ginx.RateLimit(1, 2, ginx.WithUser())).      // 1 rps, burst 2
		Use(ginx.RateLimitPerHour(50, ginx.WithUser())). // 50 per hour
		Build())
	freeGroup.GET("/search", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"tier":   "free",
			"limits": "1 rps (burst 2) + 50/hour",
		})
	})

	// Premium tier: Relaxed rate limits
	premiumGroup := r.Group("/api/premium")
	premiumGroup.Use(ginx.NewChain().
		Use(ginx.RateLimit(50, 100, ginx.WithUser())).     // 50 rps, burst 100
		Use(ginx.RateLimitPerHour(5000, ginx.WithUser())). // 5000 per hour
		Build())
	premiumGroup.GET("/search", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"tier":   "premium",
			"limits": "50 rps (burst 100) + 5000/hour",
		})
	})

	// ============================================================================
	// Example 4: Endpoint-Specific Combined Rate Limiting
	// ============================================================================
	apiGroup := r.Group("/api/v1")

	// Lightweight endpoint: High RPS + Moderate daily quota
	apiGroup.GET("/ping",
		ginx.NewChain().
			Use(ginx.RateLimit(100, 200)).     // Allow high burst traffic
			Use(ginx.RateLimitPerDay(100000)). // But with daily total limit
			Build(),
		func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status": "ok",
				"limits": "100 rps + 100k/day",
			})
		},
	)

	// Heavy endpoint: Low RPS + Low hourly quota
	apiGroup.POST("/heavy-operation",
		ginx.NewChain().
			Use(ginx.RateLimit(1, 2)).      // Only 1 request per second
			Use(ginx.RateLimitPerHour(10)). // 10 per hour
			Build(),
		func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status":  "processing",
				"limits":  "1 rps + 10/hour",
				"message": "Heavy operation started",
			})
		},
	)

	// ============================================================================
	// Example 5: Different Rate Limiting Strategies for API Versions
	// ============================================================================
	// V1 API: Traditional per-second rate limiting
	v1 := r.Group("/v1")
	v1.Use(ginx.NewChain().Use(ginx.RateLimit(10, 20)).Build())
	v1.GET("/users", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version": "v1",
			"limits":  "10 rps (burst 20)",
		})
	})

	// V2 API: Time window rate limiting
	v2 := r.Group("/v2")
	v2.Use(ginx.NewChain().
		Use(ginx.RateLimitPerMinute(100)).
		Use(ginx.RateLimitPerDay(5000)).
		Build())
	v2.GET("/users", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version": "v2",
			"limits":  "100/min + 5000/day",
		})
	})

	// V3 API: Combined rate limiting
	v3 := r.Group("/v3")
	v3.Use(ginx.NewChain().
		Use(ginx.RateLimit(20, 40)).       // RPS limiting
		Use(ginx.RateLimitPerMinute(200)). // Per-minute limiting
		Use(ginx.RateLimitPerDay(10000)).  // Per-day limiting
		Build())
	v3.GET("/users", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version": "v3",
			"limits":  "20 rps + 200/min + 10k/day",
		})
	})

	// ============================================================================
	// Example 6: Path-Based Combined Rate Limiting
	// ============================================================================
	// Each path has independent counters
	pathGroup := r.Group("/api/resources")
	pathGroup.Use(ginx.NewChain().
		Use(ginx.RateLimit(5, 10, ginx.WithPath())).      // 5 rps per path
		Use(ginx.RateLimitPerHour(100, ginx.WithPath())). // 100 per hour per path
		Build())
	pathGroup.GET("/images", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"resource": "images", "limits": "5 rps + 100/hour per path"})
	})
	pathGroup.GET("/videos", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"resource": "videos", "limits": "5 rps + 100/hour per path"})
	})

	// ============================================================================
	// Status and Documentation Endpoint
	// ============================================================================
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"title":       "Combined Rate Limiting Example",
			"description": "Demonstrates how to use RPS and time-window rate limiting together",
			"examples": map[string]interface{}{
				"example1": map[string]string{
					"url":     "/api/example1/data",
					"limits":  "10 rps (burst 20) + 100/min",
					"purpose": "Two-layer protection: prevent instant spikes and sustained abuse",
				},
				"example2": map[string]string{
					"url":     "/api/example2/premium",
					"limits":  "5 rps + 1000/hour + 10000/day",
					"purpose": "Three-layer protection: complete traffic control solution",
				},
				"example3_free": map[string]string{
					"url":     "/api/free/search",
					"limits":  "1 rps (burst 2) + 50/hour",
					"purpose": "Strict rate limits for free users",
				},
				"example3_premium": map[string]string{
					"url":     "/api/premium/search",
					"limits":  "50 rps (burst 100) + 5000/hour",
					"purpose": "Relaxed rate limits for premium users",
				},
				"example4_ping": map[string]string{
					"url":     "/api/v1/ping",
					"limits":  "100 rps + 100k/day",
					"purpose": "High burst for lightweight endpoint",
				},
				"example4_heavy": map[string]string{
					"url":     "/api/v1/heavy-operation",
					"limits":  "1 rps + 10/hour",
					"purpose": "Strict limits for heavy endpoint",
				},
				"v1": map[string]string{
					"url":     "/v1/users",
					"limits":  "10 rps only",
					"purpose": "Traditional RPS rate limiting",
				},
				"v2": map[string]string{
					"url":     "/v2/users",
					"limits":  "100/min + 5000/day",
					"purpose": "Pure time-window rate limiting",
				},
				"v3": map[string]string{
					"url":     "/v3/users",
					"limits":  "20 rps + 200/min + 10k/day",
					"purpose": "Complete combined rate limiting",
				},
			},
			"tips": []string{
				"RPS rate limiting is ideal for preventing instant traffic spikes",
				"Time-window rate limiting is ideal for quota management",
				"Combined use provides multi-layer protection",
				"Different endpoints can have different rate limiting strategies",
			},
		})
	})

	// Cleanup resources
	defer func() {
		fmt.Println("\nCleaning up rate limiters...")
		ginx.CleanupRateLimiters()
	}()

	fmt.Println("=== Combined Rate Limiting Example Server ===")
	fmt.Println("")
	fmt.Println("✨ Features:")
	fmt.Println("  - RPS rate limiting (requests per second)")
	fmt.Println("  - Time-window rate limiting (per minute/hour/day)")
	fmt.Println("  - Multi-layer combined rate limiting")
	fmt.Println("  - User tier-based rate limiting")
	fmt.Println("")
	fmt.Println("🚀 Server running at: http://localhost:8080")
	fmt.Println("")
	fmt.Println("📖 Visit http://localhost:8080/ for complete documentation")
	fmt.Println("")
	fmt.Println("🧪 Test examples:")
	fmt.Println("  curl http://localhost:8080/api/example1/data")
	fmt.Println("  curl http://localhost:8080/api/example2/premium")
	fmt.Println("  curl http://localhost:8080/v3/users")
	fmt.Println("")

	if err := r.Run(":8080"); err != nil {
		fmt.Printf("❌ Server failed to start: %v\n", err)
	}
}
