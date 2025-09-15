package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	shardedcache "github.com/simp-lee/cache"
	"github.com/simp-lee/ginx"
)

func main() {
	// Set Gin to release mode for cleaner output
	gin.SetMode(gin.ReleaseMode)

	// Create cache instance with native library configuration
	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           1000,            // Maximum items in cache
		DefaultExpiration: 5 * time.Minute, // Default expiration time
		ShardCount:        16,              // Number of shards for concurrent access
		CleanupInterval:   1 * time.Minute, // Cleanup interval for expired items
	})

	// Create Gin router
	r := gin.New()

	// Add basic middleware
	r.Use(ginx.NewChain().
		Use(ginx.Recovery()).
		Use(ginx.Logger()).
		Build())

	// Setup different caching strategies
	setupBasicCaching(r, cache)
	setupConditionalCaching(r, cache)
	setupAdvancedCaching(r, cache)
	setupCacheManagement(r, cache)

	// Start server
	fmt.Println("🚀 Cache Example Server starting on :8080")
	fmt.Println("\n📋 Available Endpoints:")
	fmt.Println("  Basic Caching:")
	fmt.Println("    GET  /basic/all        - Cache all responses")
	fmt.Println("    POST /basic/all        - POST also cached")
	fmt.Println("    GET  /basic/get-only   - Cache only GET requests")
	fmt.Println("    POST /basic/get-only   - POST request (not cached)")
	fmt.Println("    GET  /basic/grouped    - Cache with groups")
	fmt.Println("\n  Conditional Caching:")
	fmt.Println("    GET  /api/users       - Cache API GET requests")
	fmt.Println("    POST /api/users       - API POST (not cached)")
	fmt.Println("    GET  /api/health      - Health check (excluded from cache)")
	fmt.Println("    GET  /public/data     - Public data (always cached)")
	fmt.Println("    GET  /admin/secret    - Admin data (never cached)")
	fmt.Println("\n  Advanced Features:")
	fmt.Println("    GET  /advanced/query?param=value  - Query parameter caching")
	fmt.Println("    GET  /advanced/headers             - Header-based caching")
	fmt.Println("    GET  /advanced/complex             - Complex conditions")
	fmt.Println("\n  Cache Management:")
	fmt.Println("    GET    /cache/stats    - Cache statistics")
	fmt.Println("    DELETE /cache/clear    - Clear all cache")
	fmt.Println("    DELETE /cache/group/:name - Clear cache group")
	fmt.Println("\n💡 Try making requests to see caching in action!")
	fmt.Println("💡 Check response headers for cache hit indicators")

	if err := r.Run(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// setupBasicCaching demonstrates basic caching patterns
func setupBasicCaching(r *gin.Engine, cache shardedcache.CacheInterface) {
	// 1. Cache all responses regardless of method
	allGroup := r.Group("/basic/all")
	allGroup.Use(ginx.NewChain().
		Use(ginx.Cache(cache)).
		Build())

	allGroup.GET("", func(c *gin.Context) {
		// Simulate some processing time
		time.Sleep(100 * time.Millisecond)
		c.JSON(200, gin.H{
			"message":   "All responses are cached",
			"timestamp": time.Now().Unix(),
			"info":      "This response is cached regardless of method",
		})
	})

	allGroup.POST("", func(c *gin.Context) {
		time.Sleep(100 * time.Millisecond)
		c.JSON(200, gin.H{
			"message":   "POST response is also cached",
			"timestamp": time.Now().Unix(),
			"info":      "Even POST requests are cached in this endpoint",
		})
	})

	// 2. Cache only GET requests using conditions
	getOnlyGroup := r.Group("/basic/get-only")
	getOnlyGroup.Use(ginx.NewChain().
		When(ginx.MethodIs("GET"), ginx.Cache(cache)).
		Build())

	getOnlyGroup.GET("", func(c *gin.Context) {
		time.Sleep(100 * time.Millisecond)
		c.JSON(200, gin.H{
			"message":   "Only GET requests are cached",
			"timestamp": time.Now().Unix(),
			"method":    c.Request.Method,
		})
	})

	getOnlyGroup.POST("", func(c *gin.Context) {
		time.Sleep(100 * time.Millisecond)
		c.JSON(200, gin.H{
			"message":   "POST requests are NOT cached",
			"timestamp": time.Now().Unix(),
			"method":    c.Request.Method,
		})
	})

	// 3. Cache with groups
	groupedGroup := r.Group("/basic/grouped")
	groupedGroup.Use(ginx.NewChain().
		When(ginx.MethodIs("GET"), ginx.CacheWithGroup(cache, "basic-api")).
		Build())

	groupedGroup.GET("", func(c *gin.Context) {
		time.Sleep(100 * time.Millisecond)
		c.JSON(200, gin.H{
			"message":   "Cached in 'basic-api' group",
			"timestamp": time.Now().Unix(),
			"group":     "basic-api",
		})
	})
}

// setupConditionalCaching demonstrates advanced conditional caching
func setupConditionalCaching(r *gin.Engine, cache shardedcache.CacheInterface) {
	// Define conditions
	isAPIPath := ginx.PathHasPrefix("/api/")
	isPublicPath := ginx.PathHasPrefix("/public/")
	isGETRequest := ginx.MethodIs("GET")
	isNotHealthCheck := ginx.Not(ginx.PathIs("/api/health"))

	// Apply conditional caching only to specific paths
	r.Use(ginx.NewChain().
		// Cache API GET requests (except health checks)
		When(ginx.And(isAPIPath, isGETRequest, isNotHealthCheck),
			ginx.CacheWithGroup(cache, "api")).
		// Always cache public content
		When(isPublicPath, ginx.CacheWithGroup(cache, "public")).
		Build())

	// API endpoints
	r.GET("/api/users", func(c *gin.Context) {
		time.Sleep(150 * time.Millisecond)
		c.JSON(200, gin.H{
			"users":     []string{"alice", "bob", "charlie"},
			"timestamp": time.Now().Unix(),
			"cached":    "API GET requests are cached",
		})
	})

	r.POST("/api/users", func(c *gin.Context) {
		time.Sleep(150 * time.Millisecond)
		c.JSON(201, gin.H{
			"message":   "User created",
			"timestamp": time.Now().Unix(),
			"cached":    "POST requests are not cached",
		})
	})

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
			"cached":    "Health checks are excluded from cache",
		})
	})

	// Public endpoints (always cached)
	r.GET("/public/data", func(c *gin.Context) {
		time.Sleep(200 * time.Millisecond)
		c.JSON(200, gin.H{
			"data":      "public information",
			"timestamp": time.Now().Unix(),
			"cached":    "Public data is always cached",
		})
	})

	r.POST("/public/feedback", func(c *gin.Context) {
		time.Sleep(200 * time.Millisecond)
		c.JSON(200, gin.H{
			"message":   "Feedback received",
			"timestamp": time.Now().Unix(),
			"cached":    "Even POST to public endpoints are cached",
		})
	})

	// Admin endpoints (never cached)
	r.GET("/admin/secret", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"secret":    fmt.Sprintf("secret-%d", time.Now().Unix()),
			"timestamp": time.Now().Unix(),
			"cached":    "Admin endpoints are never cached",
		})
	})
}

// setupAdvancedCaching demonstrates advanced caching features
func setupAdvancedCaching(r *gin.Engine, cache shardedcache.CacheInterface) {
	advanced := r.Group("/advanced")

	// Cache with query parameter considerations
	advanced.Use(ginx.NewChain().
		When(ginx.MethodIs("GET"), ginx.Cache(cache)).
		Build())

	advanced.GET("/query", func(c *gin.Context) {
		param := c.DefaultQuery("param", "default")
		time.Sleep(100 * time.Millisecond)
		c.JSON(200, gin.H{
			"param":     param,
			"timestamp": time.Now().Unix(),
			"info":      "Different query params create different cache keys",
		})
	})

	// Header-based conditional caching
	advanced.Use(ginx.NewChain().
		// Only cache requests with specific headers
		When(ginx.And(
			ginx.MethodIs("GET"),
			ginx.HeaderExists("X-Cache-Enabled"),
		), ginx.CacheWithGroup(cache, "header-cache")).
		Build())

	advanced.GET("/headers", func(c *gin.Context) {
		time.Sleep(150 * time.Millisecond)
		cacheEnabled := c.GetHeader("X-Cache-Enabled")
		c.JSON(200, gin.H{
			"message":       "Header-based caching",
			"cache_enabled": cacheEnabled != "",
			"timestamp":     time.Now().Unix(),
			"info":          "Add 'X-Cache-Enabled' header to enable caching",
		})
	})

	// Complex conditional logic
	advanced.Use(ginx.NewChain().
		// Cache based on multiple conditions
		When(ginx.And(
			ginx.MethodIs("GET"),
			ginx.Or(
				ginx.HeaderEquals("X-User-Type", "premium"),
				ginx.Custom(func(c *gin.Context) bool {
					return c.Query("vip") == "true"
				}),
			),
		), ginx.CacheWithGroup(cache, "premium")).
		Build())

	advanced.GET("/complex", func(c *gin.Context) {
		userType := c.GetHeader("X-User-Type")
		isVIP := c.Query("vip") == "true"
		time.Sleep(200 * time.Millisecond)

		c.JSON(200, gin.H{
			"user_type": userType,
			"is_vip":    isVIP,
			"timestamp": time.Now().Unix(),
			"info":      "Cached for premium users or VIP query param",
		})
	})
}

// setupCacheManagement provides cache management endpoints
func setupCacheManagement(r *gin.Engine, cache shardedcache.CacheInterface) {
	cacheGroup := r.Group("/cache")

	// Get cache statistics
	cacheGroup.GET("/stats", func(c *gin.Context) {
		stats := cache.Stats()
		c.JSON(200, gin.H{
			"message": "Cache statistics",
			"stats":   stats,
		})
	})

	// Clear all cache
	cacheGroup.DELETE("/clear", func(c *gin.Context) {
		cache.Clear()
		c.JSON(200, gin.H{
			"message": "All cache cleared",
		})
	})

	// Clear specific cache group
	cacheGroup.DELETE("/group/:name", func(c *gin.Context) {
		groupName := c.Param("name")
		group := cache.Group(groupName)
		group.Clear()
		c.JSON(200, gin.H{
			"message": fmt.Sprintf("Cache group '%s' cleared", groupName),
			"group":   groupName,
		})
	})

	// Get cache keys
	cacheGroup.GET("/keys", func(c *gin.Context) {
		keys := cache.Keys()
		c.JSON(200, gin.H{
			"message": "All cache keys",
			"keys":    keys,
			"count":   len(keys),
		})
	})

	// Get specific group keys
	cacheGroup.GET("/group/:name/keys", func(c *gin.Context) {
		groupName := c.Param("name")
		group := cache.Group(groupName)
		keys := group.Keys()
		c.JSON(200, gin.H{
			"message": fmt.Sprintf("Keys in group '%s'", groupName),
			"group":   groupName,
			"keys":    keys,
			"count":   len(keys),
		})
	})
}
