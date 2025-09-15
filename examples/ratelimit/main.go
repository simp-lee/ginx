package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
)

func main() {
	// Set Gin to release mode for cleaner output
	gin.SetMode(gin.ReleaseMode)

	// Create Gin router
	r := gin.New()

	// Add recovery and logging middleware using ginx
	r.Use(ginx.NewChain().
		Use(ginx.Recovery()).
		Use(ginx.Logger()).
		Build())

	// Basic rate limiting examples
	setupBasicRateLimit(r)

	// Advanced rate limiting examples
	setupAdvancedRateLimit(r)

	// Dynamic rate limiting examples
	setupDynamicRateLimit(r)

	// Special features examples
	setupSpecialFeatures(r)

	// Core conditional architecture examples - showcase Ginx's unique features
	setupConditionalArchitecture(r)

	// Start server
	fmt.Println("🚀 Rate Limit Example Server starting on :8080")
	fmt.Println("\n📋 Available Endpoints:")
	fmt.Println("  Basic Examples:")
	fmt.Println("    GET  /basic/global    - Global rate limit (10 rps, burst 20)")
	fmt.Println("    GET  /basic/per-ip    - Per-IP rate limit (5 rps, burst 10)")
	fmt.Println("    GET  /basic/per-user  - Per-user rate limit (requires ?user_id=)")
	fmt.Println("    GET  /basic/per-path  - Per-path rate limit (different limits per endpoint)")
	fmt.Println("\n  Advanced Examples:")
	fmt.Println("    GET  /advanced/custom-key     - Custom key function")
	fmt.Println("    GET  /advanced/skip-admin     - Skip rate limit for admins (?admin=true)")
	fmt.Println("    GET  /advanced/no-headers     - Rate limit without HTTP headers")
	fmt.Println("    GET  /advanced/with-wait      - Rate limit with waiting (up to 2s)")
	fmt.Println("\n  Dynamic Examples:")
	fmt.Println("    GET  /dynamic/premium         - Dynamic per-user limits (?user_id=)")
	fmt.Println("    GET  /dynamic/tiered          - Tiered rate limiting by user type")
	fmt.Println("\n  Special Features:")
	fmt.Println("    GET  /special/condition       - Conditional rate limiting")
	fmt.Println("    GET  /special/metrics         - Rate limit metrics endpoint")
	fmt.Println("\n  Core Conditional Architecture:")
	fmt.Println("    GET  /conditional/smart       - Smart conditional rate limiting")
	fmt.Println("    POST /conditional/api        - API-specific conditional chains")
	fmt.Println("    GET  /conditional/complex     - Complex condition combinations")
	fmt.Println("    GET  /conditional/production  - Production-ready conditional setup")
	fmt.Println("\n💡 Try making rapid requests to see rate limiting in action!")
	fmt.Println("💡 Use tools like curl or ab (Apache Bench) for testing:")
	fmt.Println("    curl -i http://localhost:8080/basic/global")
	fmt.Println("    ab -n 100 -c 10 http://localhost:8080/basic/per-ip")

	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// setupBasicRateLimit demonstrates basic rate limiting usage
func setupBasicRateLimit(r *gin.Engine) {
	basic := r.Group("/basic")

	// 1. Global rate limit - applies to all requests from all clients
	basic.GET("/global", ginx.RateLimit(10, 20)(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Global rate limit: 10 rps, burst 20",
			"info":    "This endpoint has a global rate limit shared by all clients",
		})
	}))

	// 2. Per-IP rate limit - each IP gets its own rate limit bucket
	basic.GET("/per-ip", ginx.RateLimitByIP(5, 10)(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message":   "Per-IP rate limit: 5 rps, burst 10",
			"client_ip": c.ClientIP(),
			"info":      "Each IP address gets its own rate limit bucket",
		})
	}))

	// 3. Per-user rate limit - requires user authentication
	basic.GET("/per-user",
		extractUserID(), // Middleware to extract user_id from query
		ginx.RateLimitByUser(3, 6)(func(c *gin.Context) {
			userID, _ := c.Get("user_id")
			c.JSON(http.StatusOK, gin.H{
				"message": "Per-user rate limit: 3 rps, burst 6",
				"user_id": userID,
				"info":    "Each authenticated user gets their own rate limit",
			})
		}))

	// 4. Per-path rate limit - different limits for different endpoints
	basic.GET("/per-path", ginx.RateLimitByPath(2, 4)(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Per-path rate limit: 2 rps, burst 4",
			"path":    c.Request.URL.Path,
			"info":    "Each client gets separate limits for each endpoint",
		})
	}))
}

// setupAdvancedRateLimit demonstrates advanced configuration options
func setupAdvancedRateLimit(r *gin.Engine) {
	advanced := r.Group("/advanced")

	// 1. Custom key function - group requests by custom logic
	customKeyLimiter := ginx.NewRateLimiter(8, 15).
		WithKeyFunc(func(c *gin.Context) string {
			// Group by user agent + IP for more granular control
			return fmt.Sprintf("%s:%s", c.ClientIP(), c.GetHeader("User-Agent"))
		})

	advanced.GET("/custom-key", customKeyLimiter.Middleware()(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message":    "Custom key function: 8 rps, burst 15",
			"key_basis":  "IP + User-Agent",
			"user_agent": c.GetHeader("User-Agent"),
			"info":       "Rate limiting based on IP + User-Agent combination",
		})
	}))

	// 2. Skip function - exempt certain requests
	skipAdminLimiter := ginx.NewRateLimiter(4, 8).
		WithSkipFunc(func(c *gin.Context) bool {
			// Skip rate limiting for admin users
			return c.Query("admin") == "true"
		})

	advanced.GET("/skip-admin", skipAdminLimiter.Middleware()(func(c *gin.Context) {
		isAdmin := c.Query("admin") == "true"
		c.JSON(http.StatusOK, gin.H{
			"message":  "Skip function: 4 rps, burst 8",
			"is_admin": isAdmin,
			"info":     "Add ?admin=true to skip rate limiting",
		})
	}))

	// 3. Disable headers - no X-RateLimit-* headers in response
	noHeadersLimiter := ginx.NewRateLimiter(6, 12).WithoutHeaders()

	advanced.GET("/no-headers", noHeadersLimiter.Middleware()(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "No headers: 6 rps, burst 12",
			"info":    "Rate limit info is not included in response headers",
		})
	}))

	// 4. Rate limiting with waiting - smooths out traffic spikes
	advanced.GET("/with-wait", ginx.RateLimitWithWait(3, 5, 2*time.Second)(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "With wait: 3 rps, burst 5, max wait 2s",
			"info":    "Requests wait up to 2 seconds for available tokens",
		})
	}))
}

// setupDynamicRateLimit demonstrates dynamic rate limiting
func setupDynamicRateLimit(r *gin.Engine) {
	dynamic := r.Group("/dynamic")

	// 1. Premium user rate limiting
	dynamic.GET("/premium",
		extractUserID(),
		ginx.RateLimitPerUser(func(userID string) (rps int, burst int) {
			if isPremiumUser(userID) {
				return 50, 100 // Premium users get higher limits
			}
			return 10, 20 // Regular users
		})(func(c *gin.Context) {
			userID, _ := c.Get("user_id")
			isPremium := isPremiumUser(userID.(string))

			limits := "10 rps, 20 burst"
			if isPremium {
				limits = "50 rps, 100 burst"
			}

			c.JSON(http.StatusOK, gin.H{
				"message":    "Dynamic premium limits",
				"user_id":    userID,
				"is_premium": isPremium,
				"limits":     limits,
				"info":       "Premium users get higher rate limits",
			})
		}))

	// 2. Tiered rate limiting based on user plan
	// Since DynamicRateLimiter uses user ID by default, we'll simulate different user types
	tieredLimiter := ginx.NewDynamicRateLimiter(func(userID string) (rps int, burst int) {
		// For demo purposes, check query param to determine user type
		// In real apps, this would query user database
		if len(userID) > 0 && userID[0] == 'e' { // enterprise users start with 'e'
			return 200, 400
		} else if len(userID) > 0 && userID[0] == 'p' { // premium users start with 'p'
			return 50, 100
		} else if len(userID) > 0 && userID[0] == 'b' { // basic users start with 'b'
			return 10, 20
		}
		return 5, 10 // Free tier
	})

	dynamic.GET("/tiered",
		extractUserID(),
		tieredLimiter.Middleware()(func(c *gin.Context) {
			userID, _ := c.Get("user_id")
			userIDStr := userID.(string)

			var limits, userType string
			if len(userIDStr) > 0 {
				switch userIDStr[0] {
				case 'e':
					userType = "enterprise"
					limits = "200 rps, 400 burst"
				case 'p':
					userType = "premium"
					limits = "50 rps, 100 burst"
				case 'b':
					userType = "basic"
					limits = "10 rps, 20 burst"
				default:
					userType = "free"
					limits = "5 rps, 10 burst"
				}
			} else {
				userType = "free"
				limits = "5 rps, 10 burst"
			}

			c.JSON(http.StatusOK, gin.H{
				"message":   "Tiered rate limiting",
				"user_id":   userIDStr,
				"user_type": userType,
				"limits":    limits,
				"info":      "Try ?user_id=enterprise1, premium1, or basic1 for different limits",
			})
		}))
}

// setupSpecialFeatures demonstrates special rate limiting features
func setupSpecialFeatures(r *gin.Engine) {
	special := r.Group("/special")

	// Create a store for condition checking
	store := ginx.NewMemoryLimiterStore(5 * time.Minute)
	defer store.Close()

	// 1. Conditional rate limiting using middleware chains
	chain := ginx.NewChain().
		Use(func(next gin.HandlerFunc) gin.HandlerFunc {
			return func(c *gin.Context) {
				// Add warning header for demonstration
				c.Header("X-Warning", "Rate limiting active")
				next(c)
			}
		}).
		Use(ginx.RateLimit(5, 10))

	special.GET("/condition", chain.Build(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Conditional rate limiting",
			"info":    "Warning header added when approaching rate limit",
		})
	})

	// 2. Rate limit metrics endpoint
	special.GET("/metrics", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Rate limit metrics (mock)",
			"metrics": gin.H{
				"total_requests":  12345,
				"rate_limited":    234,
				"success_rate":    "98.1%",
				"active_limiters": 45,
				"memory_usage":    "2.1MB",
				"cleanup_cycles":  67,
			},
			"info": "This would show real metrics in a production system",
		})
	})
}

// setupConditionalArchitecture demonstrates Ginx's core conditional architecture
// This showcases the unique "极简 + 可组合 + 条件执行" design philosophy
func setupConditionalArchitecture(r *gin.Engine) {
	conditional := r.Group("/conditional")

	// 1. Smart conditional rate limiting - showcase When/Unless with condition DSL
	smartChain := ginx.NewChain().
		OnError(func(c *gin.Context, err error) {
			c.JSON(500, gin.H{"error": "middleware error", "details": err.Error()})
		}).
		Use(ginx.Recovery()).
		// Only apply rate limiting for non-health check paths
		When(ginx.Not(ginx.PathIs("/conditional/smart/health")), ginx.RateLimit(5, 10)).
		// Apply strict limits only for POST/PUT/DELETE methods
		When(ginx.MethodIs("POST", "PUT", "DELETE"), ginx.RateLimit(2, 4)).
		// Skip rate limiting for admin users
		Unless(ginx.HeaderEquals("X-Admin", "true"), ginx.RateLimit(1, 2))

	conditional.GET("/smart", smartChain.Build(), func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Smart conditional rate limiting",
			"info":    "Rate limiting applied based on path, method, and user type",
			"applied_conditions": []string{
				"Not health check path",
				"Method-based limits",
				"Admin bypass check",
			},
		})
	})

	conditional.GET("/smart/health", smartChain.Build(), func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"message": "Health check - no rate limiting applied",
		})
	})

	// 2. API-specific conditional chains with complex conditions
	// Define complex condition combinations first
	isAPIRequest := ginx.PathHasPrefix("/conditional/api")
	isAuthenticatedAPI := ginx.And(
		isAPIRequest,
		ginx.HeaderExists("Authorization"),
	)
	isPublicAPI := ginx.And(
		isAPIRequest,
		ginx.Not(ginx.HeaderExists("Authorization")),
	)
	isHighVolumeAPI := ginx.Or(
		ginx.PathHasSuffix("/bulk"),
		ginx.PathHasSuffix("/batch"),
		ginx.PathHasSuffix("/upload"),
	)

	apiChain := ginx.NewChain().
		Use(ginx.Recovery()).
		// Public API gets basic rate limiting
		When(isPublicAPI, ginx.RateLimit(10, 20)).
		// Authenticated API gets higher limits
		When(isAuthenticatedAPI, ginx.RateLimit(50, 100)).
		// High volume endpoints get special treatment
		When(isHighVolumeAPI, ginx.RateLimit(100, 200)).
		// Apply timeout only for authenticated high-volume requests
		When(ginx.And(isAuthenticatedAPI, isHighVolumeAPI), ginx.Timeout(ginx.WithTimeout(60*time.Second)))

	conditional.POST("/api/data", apiChain.Build(), func(c *gin.Context) {
		hasAuth := c.GetHeader("Authorization") != ""
		c.JSON(200, gin.H{
			"message":       "API endpoint with conditional chains",
			"authenticated": hasAuth,
			"rate_limits":   map[string]string{"public": "10/20", "authenticated": "50/100"},
		})
	})

	conditional.POST("/api/bulk", apiChain.Build(), func(c *gin.Context) {
		hasAuth := c.GetHeader("Authorization") != ""
		limits := "10/20"
		if hasAuth {
			limits = "100/200 (high volume + auth)"
		}
		c.JSON(200, gin.H{
			"message":       "High volume API endpoint",
			"authenticated": hasAuth,
			"rate_limits":   limits,
			"timeout":       "60s for authenticated requests",
		})
	})

	// 3. Complex condition combinations - real-world scenarios
	complexChain := ginx.NewChain().
		OnError(func(c *gin.Context, err error) {
			c.JSON(500, gin.H{"error": "complex chain error"})
		}).
		Use(ginx.Recovery()).
		// Skip everything for health/status endpoints
		Unless(ginx.Or(
			ginx.PathHasSuffix("/health"),
			ginx.PathHasSuffix("/status"),
			ginx.PathHasSuffix("/metrics"),
		), ginx.RateLimit(5, 10)).
		// Apply different limits based on content type
		When(ginx.ContentTypeIs("application/json"), ginx.RateLimit(20, 40)).
		When(ginx.ContentTypeIs("multipart/form-data"), ginx.RateLimit(5, 10)).
		// Browser requests get different treatment
		When(ginx.Custom(func(c *gin.Context) bool {
			ua := c.GetHeader("User-Agent")
			return strings.Contains(ua, "Mozilla") || strings.Contains(ua, "Chrome")
		}), ginx.RateLimit(30, 60))

	conditional.GET("/complex", complexChain.Build(), func(c *gin.Context) {
		conditions := []string{}

		// Check which conditions would apply
		if !strings.HasSuffix(c.Request.URL.Path, "/health") {
			conditions = append(conditions, "Base rate limiting (5/10)")
		}

		ct := c.GetHeader("Content-Type")
		if strings.Contains(ct, "application/json") {
			conditions = append(conditions, "JSON rate limiting (20/40)")
		}

		ua := c.GetHeader("User-Agent")
		if strings.Contains(ua, "Mozilla") || strings.Contains(ua, "Chrome") {
			conditions = append(conditions, "Browser rate limiting (30/60)")
		}

		c.JSON(200, gin.H{
			"message":            "Complex conditional chains",
			"user_agent":         ua,
			"content_type":       ct,
			"applied_conditions": conditions,
			"info":               "Different rate limits based on multiple conditions",
		})
	})

	// 4. Production-ready conditional setup - showcase real-world usage
	productionChain := ginx.NewChain().
		OnError(func(c *gin.Context, err error) {
			// Log error in production, return generic message
			c.JSON(500, gin.H{"error": "internal server error"})
		}).
		Use(ginx.Recovery()).
		// Global base protection
		Use(ginx.RateLimit(100, 200)).
		// Strict limits for anonymous users
		Unless(ginx.HeaderExists("Authorization"), ginx.RateLimit(10, 20)).
		// Skip rate limiting for internal services
		Unless(ginx.Or(
			ginx.HeaderEquals("X-Internal-Service", "true"),
			ginx.PathHasPrefix("/internal/"),
		), ginx.RateLimit(5, 10)).
		// Apply timeout for external API calls
		When(ginx.PathHasPrefix("/external/"), ginx.Timeout(ginx.WithTimeout(30*time.Second))).
		// Different limits for different API versions
		When(ginx.PathHasPrefix("/v1/"), ginx.RateLimit(50, 100)).
		When(ginx.PathHasPrefix("/v2/"), ginx.RateLimit(100, 200))

	conditional.GET("/production", productionChain.Build(), func(c *gin.Context) {
		hasAuth := c.GetHeader("Authorization") != ""
		isInternal := c.GetHeader("X-Internal-Service") == "true"

		appliedLimits := []string{"Base: 100/200"}
		if !hasAuth {
			appliedLimits = append(appliedLimits, "Anonymous: 10/20")
		}
		if !isInternal {
			appliedLimits = append(appliedLimits, "External: 5/10")
		}

		c.JSON(200, gin.H{
			"message":          "Production-ready conditional setup",
			"authenticated":    hasAuth,
			"internal_service": isInternal,
			"applied_limits":   appliedLimits,
			"info":             "Real-world conditional rate limiting patterns",
		})
	})
}

// Helper middleware to extract user_id from query parameters
func extractUserID() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Query("user_id")
		if userID == "" {
			userID = "anonymous"
		}
		c.Set("user_id", userID)
		c.Next()
	}
}

// Helper function to determine if a user is premium
func isPremiumUser(userID string) bool {
	// Simple logic for demo - in real app this would check a database
	premiumUsers := map[string]bool{
		"premium1": true,
		"premium2": true,
		"vip":      true,
	}
	return premiumUsers[userID]
}
