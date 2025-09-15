package ginx

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	shardedcache "github.com/simp-lee/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_BasicFunctionality(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	responseBody := `{"message":"test"}`
	r.GET("/test", func(c *gin.Context) {
		c.Header("Custom-Header", "custom-value")
		c.JSON(200, gin.H{"message": "test"})
	})

	// First request - cache miss
	req1 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, 200, w1.Code)
	assert.Contains(t, w1.Body.String(), responseBody)
	assert.Equal(t, "custom-value", w1.Header().Get("Custom-Header"))

	// Second request - cache hit
	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), responseBody)
	assert.Equal(t, "custom-value", w2.Header().Get("Custom-Header"))

	// Verify cache is actually working
	assert.True(t, cache.Has("/test"))
}

func TestCache_ConditionalCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()

	// Use condition architecture: cache only GET requests
	r.Use(NewChain().
		When(MethodIs("GET"), Cache(cache)).
		Build())

	handler := func(c *gin.Context) {
		c.JSON(200, gin.H{"method": c.Request.Method})
	}

	r.GET("/test", handler)
	r.POST("/test", handler)
	r.PUT("/test", handler)
	r.DELETE("/test", handler)

	// GET requests should be cached
	reqGet := httptest.NewRequest("GET", "/test", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	assert.Equal(t, 200, wGet.Code)
	assert.True(t, cache.Has("/test"), "GET requests should be cached")

	methods := []string{"POST", "PUT", "DELETE"}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		// Verify non-GET requests don't interfere with cache (condition mismatch, cache middleware not executed)
	}

	// Verify cache still exists (only GET request cache)
	assert.True(t, cache.Has("/test"))
}

func TestCache_QueryParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	r.GET("/test", func(c *gin.Context) {
		param := c.Query("param")
		c.JSON(200, gin.H{"param": param})
	})

	// Request 1: /test?param=value1
	req1 := httptest.NewRequest("GET", "/test?param=value1", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, 200, w1.Code)
	assert.Contains(t, w1.Body.String(), "value1")

	// Request 2: /test?param=value2 (different query parameters)
	req2 := httptest.NewRequest("GET", "/test?param=value2", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), "value2")

	// Verify two different cache keys are created
	assert.True(t, cache.Has("/test?param=value1"))
	assert.True(t, cache.Has("/test?param=value2"))
}

func TestCache_StatusCodeFiltering(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	// Test different status codes
	testCases := []struct {
		path        string
		statusCode  int
		shouldCache bool
	}{
		{"/ok", 200, true},
		{"/created", 201, true},
		{"/accepted", 202, true},
		{"/no-content", 204, true},
		{"/moved", 299, true},
		{"/redirect", 301, false},
		{"/not-found", 404, false},
		{"/error", 500, false},
	}

	for _, tc := range testCases {
		r.GET(tc.path, func(c *gin.Context) {
			c.JSON(tc.statusCode, gin.H{"status": tc.statusCode})
		})

		req := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, tc.statusCode, w.Code)

		if tc.shouldCache {
			assert.True(t, cache.Has(tc.path), "Status %d should be cached", tc.statusCode)
		} else {
			assert.False(t, cache.Has(tc.path), "Status %d should not be cached", tc.statusCode)
		}
	}
}

func TestCacheWithGroup_BasicFunctionality(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()

	// Use cache group
	r.Use(NewChain().Use(CacheWithGroup(cache, "api-v1")).Build())

	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"version": "v1"})
	})

	// First request
	req1 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, 200, w1.Code)
	assert.Contains(t, w1.Body.String(), "v1")

	// Verify cache is in group
	group := cache.Group("api-v1")
	assert.True(t, group.Has("/test"))

	// Main cache should not have this key (because it's in a group)
	assert.False(t, cache.Has("/test"))

	// Second request - should hit group cache
	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), "v1")
}

func TestCacheWithGroup_IsolationBetweenGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	// Create routes for two different groups
	r1 := gin.New()
	r1.Use(NewChain().Use(CacheWithGroup(cache, "group1")).Build())
	r1.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"group": "1"})
	})

	r2 := gin.New()
	r2.Use(NewChain().Use(CacheWithGroup(cache, "group2")).Build())
	r2.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"group": "2"})
	})

	// Request to group 1
	req1 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	r1.ServeHTTP(w1, req1)
	assert.Contains(t, w1.Body.String(), `"group":"1"`)

	// Request to group 2
	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)
	assert.Contains(t, w2.Body.String(), `"group":"2"`)

	// Verify group isolation
	group1 := cache.Group("group1")
	group2 := cache.Group("group2")

	assert.True(t, group1.Has("/test"))
	assert.True(t, group2.Has("/test"))

	// Clearing group1 cache should not affect group2
	group1.Clear()
	assert.False(t, group1.Has("/test"))
	assert.True(t, group2.Has("/test"))
}

func TestCache_EmptyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	r.GET("/empty", func(c *gin.Context) {
		c.Status(204) // No Content
	})

	req := httptest.NewRequest("GET", "/empty", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 204, w.Code)
	assert.Empty(t, w.Body.String())
	assert.True(t, cache.Has("/empty"))

	// Request again to verify cache
	req2 := httptest.NewRequest("GET", "/empty", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 204, w2.Code)
	assert.Empty(t, w2.Body.String())
}

func TestCache_LargeResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           10,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	// Create a large response
	largeData := make([]byte, 1024*10) // 10KB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	r.GET("/large", func(c *gin.Context) {
		c.Data(200, "application/octet-stream", largeData)
	})

	req := httptest.NewRequest("GET", "/large", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, len(largeData), len(w.Body.Bytes()))
	assert.True(t, cache.Has("/large"))
}

func TestCache_ConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	counter := 0
	r.GET("/concurrent", func(c *gin.Context) {
		counter++
		time.Sleep(10 * time.Millisecond) // Simulate some processing time
		c.JSON(200, gin.H{"count": counter})
	})

	// Send multiple concurrent requests
	const numRequests = 10
	results := make(chan *httptest.ResponseRecorder, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/concurrent", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			results <- w
		}()
	}

	// Collect results
	responses := make([]*httptest.ResponseRecorder, 0, numRequests)
	for i := 0; i < numRequests; i++ {
		responses = append(responses, <-results)
	}

	// Verify all responses are successful
	for _, w := range responses {
		assert.Equal(t, 200, w.Code)
	}

	// Verify cache exists
	assert.True(t, cache.Has("/concurrent"))
}

func TestGenerateCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name     string
		path     string
		query    string
		expected string
	}{
		{
			name:     "path only",
			path:     "/api/users",
			query:    "",
			expected: "/api/users",
		},
		{
			name:     "path with query",
			path:     "/api/users",
			query:    "page=1&limit=10",
			expected: "/api/users?page=1&limit=10",
		},
		{
			name:     "root path",
			path:     "/",
			query:    "",
			expected: "/",
		},
		{
			name:     "complex query",
			path:     "/search",
			query:    "q=golang&sort=date&order=desc",
			expected: "/search?q=golang&sort=date&order=desc",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path+"?"+tc.query, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req

			key := generateCacheKey(c)
			assert.Equal(t, tc.expected, key)
		})
	}
}

func TestResponseWriter_Write(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	rw := &responseWriter{
		ResponseWriter: c.Writer,
		cache:          cache,
		groupName:      "",
		key:            "test-key",
		body:           make([]byte, 0),
	}

	// Test Write method
	data := []byte("test data")
	n, err := rw.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, rw.body)
}

func TestCache_MultipleHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	r.GET("/headers", func(c *gin.Context) {
		c.Header("Header1", "value1")
		c.Header("Header2", "value2")
		c.Header("Content-Type", "application/json")
		c.JSON(200, gin.H{"test": "headers"})
	})

	// First request
	req1 := httptest.NewRequest("GET", "/headers", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, 200, w1.Code)
	assert.Equal(t, "value1", w1.Header().Get("Header1"))
	assert.Equal(t, "value2", w1.Header().Get("Header2"))

	// Second request - retrieved from cache
	req2 := httptest.NewRequest("GET", "/headers", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 200, w2.Code)
	assert.Equal(t, "value1", w2.Header().Get("Header1"))
	assert.Equal(t, "value2", w2.Header().Get("Header2"))
	assert.Contains(t, w2.Body.String(), `"test":"headers"`)
}

func TestCache_ComplexConditionalStrategies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()

	// Complex condition combinations:
	// 1. Cache only GET requests for API paths
	// 2. Exclude /api/health health checks
	// 3. /api/users uses dedicated cache group
	isAPIPath := PathHasPrefix("/api/")
	isGETRequest := MethodIs("GET")
	isNotHealth := Not(PathIs("/api/health"))
	isUsersEndpoint := PathIs("/api/users")

	r.Use(NewChain().
		// Basic API caching
		When(And(isAPIPath, isGETRequest, isNotHealth), Cache(cache)).
		// Users API uses dedicated cache group
		When(And(isUsersEndpoint, isGETRequest), CacheWithGroup(cache, "json-api")).
		Build())

	// Setup routes
	r.GET("/api/users", func(c *gin.Context) {
		c.JSON(200, gin.H{"users": []string{"user1", "user2"}})
	})

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.GET("/static/file", func(c *gin.Context) {
		c.String(200, "static content")
	})

	r.POST("/api/users", func(c *gin.Context) {
		c.JSON(201, gin.H{"message": "created"})
	})

	// Test 1: API GET requests should be cached
	req1 := httptest.NewRequest("GET", "/api/users", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)
	assert.True(t, cache.Has("/api/users"), "API GET should be cached")

	// Test 2: Health checks should not be cached
	req2 := httptest.NewRequest("GET", "/api/health", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.False(t, cache.Has("/api/health"), "Health check should not be cached")

	// Test 3: Non-API paths should not be cached
	req3 := httptest.NewRequest("GET", "/static/file", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
	assert.False(t, cache.Has("/static/file"), "Non-API path should not be cached")

	// Test 4: POST requests should not be cached
	req4 := httptest.NewRequest("POST", "/api/users", nil)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	assert.Equal(t, 201, w4.Code)
	// POST request uses same path but doesn't override GET cache
	assert.True(t, cache.Has("/api/users"), "GET cache should remain")

	// Verify cache group usage
	jsonGroup := cache.Group("json-api")
	assert.True(t, jsonGroup.Has("/api/users"), "JSON API should be in group cache")
}

func TestCache_PathBasedStrategies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()

	// Different paths use different caching strategies
	r.Use(NewChain().
		// Cache all requests under /public/
		When(PathHasPrefix("/public/"), Cache(cache)).
		// Cache only GET and HEAD under /api/
		When(And(PathHasPrefix("/api/"), Or(MethodIs("GET"), MethodIs("HEAD"))), CacheWithGroup(cache, "api")).
		// Do not cache /admin/ paths
		Unless(PathHasPrefix("/admin/"), Cache(cache)).
		Build())

	// Setup test routes
	r.GET("/public/data", func(c *gin.Context) {
		c.JSON(200, gin.H{"data": "public"})
	})
	r.POST("/public/upload", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "uploaded"})
	})
	r.GET("/api/users", func(c *gin.Context) {
		c.JSON(200, gin.H{"users": []int{1, 2, 3}})
	})
	r.POST("/api/users", func(c *gin.Context) {
		c.JSON(201, gin.H{"created": true})
	})
	r.GET("/admin/stats", func(c *gin.Context) {
		c.JSON(200, gin.H{"stats": "secret"})
	})

	// Test that all methods under public path are cached
	req1 := httptest.NewRequest("GET", "/public/data", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.True(t, cache.Has("/public/data"))

	req2 := httptest.NewRequest("POST", "/public/upload", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.True(t, cache.Has("/public/upload"))

	// Test that API path only caches GET
	req3 := httptest.NewRequest("GET", "/api/users", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	apiGroup := cache.Group("api")
	assert.True(t, apiGroup.Has("/api/users"))

	req4 := httptest.NewRequest("POST", "/api/users", nil)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	assert.True(t, apiGroup.Has("/api/users"), "GET cache should remain after POST request")

	// Test that admin path is not cached
	req5 := httptest.NewRequest("GET", "/admin/stats", nil)
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	assert.False(t, cache.Has("/admin/stats"))
}
