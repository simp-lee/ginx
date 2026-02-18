package ginx

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	shardedcache "github.com/simp-lee/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultTestCacheKey(method, target string) string {
	req := httptest.NewRequest(method, target, nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return generateCacheKey(c)
}

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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/test")))
}

func TestCacheMiddleware_NilCachePanics(t *testing.T) {
	assert.PanicsWithValue(t, "cache middleware requires non-nil cache backend", func() {
		Cache(nil)
	})

	assert.PanicsWithValue(t, "cache middleware requires non-nil cache backend", func() {
		CacheWithOptions(nil)
	})

	assert.PanicsWithValue(t, "cache middleware requires non-nil cache backend", func() {
		CacheWithGroup(nil, "group")
	})

	assert.PanicsWithValue(t, "cache middleware requires non-nil cache backend", func() {
		CacheWithGroupOptions(nil, "group")
	})
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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/test")), "GET requests should be cached")

	methods := []string{"POST", "PUT", "DELETE"}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		// Verify non-GET requests don't interfere with cache (condition mismatch, cache middleware not executed)
	}

	// Verify cache still exists (only GET request cache)
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/test")))
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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/test?param=value1")))
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/test?param=value2")))
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
			assert.True(t, cache.Has(defaultTestCacheKey("GET", tc.path)), "Status %d should be cached", tc.statusCode)
		} else {
			assert.False(t, cache.Has(defaultTestCacheKey("GET", tc.path)), "Status %d should not be cached", tc.statusCode)
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
	assert.True(t, group.Has(defaultTestCacheKey("GET", "/test")))

	// Main cache should not have this key (because it's in a group)
	assert.False(t, cache.Has(defaultTestCacheKey("GET", "/test")))

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

	assert.True(t, group1.Has(defaultTestCacheKey("GET", "/test")))
	assert.True(t, group2.Has(defaultTestCacheKey("GET", "/test")))

	// Clearing group1 cache should not affect group2
	group1.Clear()
	assert.False(t, group1.Has(defaultTestCacheKey("GET", "/test")))
	assert.True(t, group2.Has(defaultTestCacheKey("GET", "/test")))
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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/empty")))

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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/large")))
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

	var counter int64
	r.GET("/concurrent", func(c *gin.Context) {
		// Use atomic operation to safely increment counter
		count := atomic.AddInt64(&counter, 1)
		time.Sleep(10 * time.Millisecond) // Simulate some processing time
		c.JSON(200, gin.H{"count": count})
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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/concurrent")))
}

func TestGenerateCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name     string
		method   string
		path     string
		query    string
		expected string
	}{
		{
			name:     "GET path only",
			method:   "GET",
			path:     "/api/users",
			query:    "",
			expected: "GET|example.com|/api/users",
		},
		{
			name:     "GET path with query",
			method:   "GET",
			path:     "/api/users",
			query:    "page=1&limit=10",
			expected: "GET|example.com|/api/users?page=1&limit=10",
		},
		{
			name:     "POST path only",
			method:   "POST",
			path:     "/api/users",
			query:    "",
			expected: "POST|example.com|/api/users",
		},
		{
			name:     "POST path with query",
			method:   "POST",
			path:     "/api/users",
			query:    "debug=true",
			expected: "POST|example.com|/api/users?debug=true",
		},
		{
			name:     "GET root path",
			method:   "GET",
			path:     "/",
			query:    "",
			expected: "GET|example.com|/",
		},
		{
			name:     "PUT with complex query",
			method:   "PUT",
			path:     "/search",
			query:    "q=golang&sort=date&order=desc",
			expected: "PUT|example.com|/search?q=golang&sort=date&order=desc",
		},
		{
			name:     "DELETE path only",
			method:   "DELETE",
			path:     "/api/users/123",
			query:    "",
			expected: "DELETE|example.com|/api/users/123",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var url string
			if tc.query != "" {
				url = tc.path + "?" + tc.query
			} else {
				url = tc.path
			}

			req := httptest.NewRequest(tc.method, url, nil)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req

			key := generateCacheKey(c)
			assert.Equal(t, tc.expected, key)
		})
	}
}

// TestGenerateCacheKey_MethodDifferentiation tests that different HTTP methods generate different cache keys
func TestGenerateCacheKey_MethodDifferentiation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	path := "/api/users"
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	keys := make(map[string]string)

	for _, method := range methods {
		req := httptest.NewRequest(method, path, nil)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req

		key := generateCacheKey(c)

		// Ensure each method generates a unique key
		for existingMethod, existingKey := range keys {
			assert.NotEqual(t, existingKey, key,
				"Method %s should generate different key than %s for same path",
				method, existingMethod)
		}

		keys[method] = key

		// Verify key format
		expected := method + "|example.com|" + path
		assert.Equal(t, expected, key)
	}
}

func TestGenerateCacheKey_DefaultVaryAcceptEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestIdentity := httptest.NewRequest(http.MethodGet, "/api/content", nil)
	requestIdentity.Host = "example.com"
	contextIdentity, _ := gin.CreateTestContext(httptest.NewRecorder())
	contextIdentity.Request = requestIdentity

	requestGzip := httptest.NewRequest(http.MethodGet, "/api/content", nil)
	requestGzip.Host = "example.com"
	requestGzip.Header.Set("Accept-Encoding", "gzip")
	contextGzip, _ := gin.CreateTestContext(httptest.NewRecorder())
	contextGzip.Request = requestGzip

	identityKey := generateCacheKey(contextIdentity)
	gzipKey := generateCacheKey(contextGzip)

	assert.NotEqual(t, identityKey, gzipKey, "default cache key should isolate content-encoding variants")
	assert.Equal(t, "GET|example.com|/api/content", identityKey)
	assert.Equal(t, "GET|example.com|/api/content|h:Accept-Encoding=gzip", gzipKey)
}

func TestCache_WithCacheVaryHeaders_IsolatesLanguageVariants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(CacheWithOptions(cache,
		WithCacheVaryHeaders("Accept-Encoding", "Accept-Language"),
	)).Build())

	var callCount int32
	r.GET("/welcome", func(c *gin.Context) {
		count := atomic.AddInt32(&callCount, 1)
		c.JSON(http.StatusOK, gin.H{
			"lang":  c.GetHeader("Accept-Language"),
			"count": count,
		})
	})

	requestEn1 := httptest.NewRequest(http.MethodGet, "/welcome", nil)
	requestEn1.Host = "example.com"
	requestEn1.Header.Set("Accept-Language", "en")
	responseEn1 := httptest.NewRecorder()
	r.ServeHTTP(responseEn1, requestEn1)
	assert.Equal(t, http.StatusOK, responseEn1.Code)
	assert.Contains(t, responseEn1.Body.String(), `"lang":"en"`)
	assert.Contains(t, responseEn1.Body.String(), `"count":1`)

	requestZh1 := httptest.NewRequest(http.MethodGet, "/welcome", nil)
	requestZh1.Host = "example.com"
	requestZh1.Header.Set("Accept-Language", "zh-CN")
	responseZh1 := httptest.NewRecorder()
	r.ServeHTTP(responseZh1, requestZh1)
	assert.Equal(t, http.StatusOK, responseZh1.Code)
	assert.Contains(t, responseZh1.Body.String(), `"lang":"zh-CN"`)
	assert.Contains(t, responseZh1.Body.String(), `"count":2`)

	requestEn2 := httptest.NewRequest(http.MethodGet, "/welcome", nil)
	requestEn2.Host = "example.com"
	requestEn2.Header.Set("Accept-Language", "en")
	responseEn2 := httptest.NewRecorder()
	r.ServeHTTP(responseEn2, requestEn2)
	assert.Equal(t, http.StatusOK, responseEn2.Code)
	assert.Contains(t, responseEn2.Body.String(), `"lang":"en"`)
	assert.Contains(t, responseEn2.Body.String(), `"count":1`)

	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount), "language variants should use separate cache entries")
	assert.True(t, cache.Has("GET|example.com|/welcome|h:Accept-Language=en"))
	assert.True(t, cache.Has("GET|example.com|/welcome|h:Accept-Language=zh-CN"))
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

type partialWriteResponseWriter struct {
	gin.ResponseWriter
	limit int
}

func (w *partialWriteResponseWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit {
		data = data[:w.limit]
	}
	return w.ResponseWriter.Write(data)
}

func TestResponseWriter_Write_PartialWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	rw := &responseWriter{
		ResponseWriter: &partialWriteResponseWriter{
			ResponseWriter: c.Writer,
			limit:          4,
		},
		cache:     cache,
		groupName: "",
		key:       "test-key",
		body:      make([]byte, 0),
	}

	data := []byte("test data")
	n, err := rw.Write(data)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, data[:4], rw.body)
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

func TestCache_HeadCacheHit_NoBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	r.HEAD("/head", func(c *gin.Context) {
		c.Header("X-Head-Cache", "ok")
		c.String(http.StatusOK, "head-payload")
	})

	// First request stores a cached response for HEAD
	req1 := httptest.NewRequest(http.MethodHead, "/head", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, "ok", w1.Header().Get("X-Head-Cache"))
	assert.True(t, cache.Has(defaultTestCacheKey("HEAD", "/head")))

	// Second request should hit cache and still keep HEAD semantics (no body)
	req2 := httptest.NewRequest(http.MethodHead, "/head", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "ok", w2.Header().Get("X-Head-Cache"))
	assert.Empty(t, w2.Body.String())
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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/api/users")), "API GET should be cached")

	// Test 2: Health checks should not be cached
	req2 := httptest.NewRequest("GET", "/api/health", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.False(t, cache.Has(defaultTestCacheKey("GET", "/api/health")), "Health check should not be cached")

	// Test 3: Non-API paths should not be cached
	req3 := httptest.NewRequest("GET", "/static/file", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
	assert.False(t, cache.Has(defaultTestCacheKey("GET", "/static/file")), "Non-API path should not be cached")

	// Test 4: POST requests should not be cached
	req4 := httptest.NewRequest("POST", "/api/users", nil)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	assert.Equal(t, 201, w4.Code)
	// POST request uses same path but doesn't override GET cache
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/api/users")), "GET cache should remain")

	// Verify cache group usage
	jsonGroup := cache.Group("json-api")
	assert.True(t, jsonGroup.Has(defaultTestCacheKey("GET", "/api/users")), "JSON API should be in group cache")
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
	assert.True(t, cache.Has(defaultTestCacheKey("GET", "/public/data")))

	req2 := httptest.NewRequest("POST", "/public/upload", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.False(t, cache.Has(defaultTestCacheKey("POST", "/public/upload")), "POST requests should not be cached")

	// Test that API path only caches GET
	req3 := httptest.NewRequest("GET", "/api/users", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	apiGroup := cache.Group("api")
	assert.True(t, apiGroup.Has(defaultTestCacheKey("GET", "/api/users")))

	req4 := httptest.NewRequest("POST", "/api/users", nil)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	assert.True(t, apiGroup.Has(defaultTestCacheKey("GET", "/api/users")), "GET cache should remain after POST request")

	// Test that admin path is not cached
	req5 := httptest.NewRequest("GET", "/admin/stats", nil)
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	assert.False(t, cache.Has(defaultTestCacheKey("GET", "/admin/stats")))
}

func TestCache_DefaultKeyIncludesHost_IsolatesVariants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	var callCount int32
	r.GET("/tenant-data", func(c *gin.Context) {
		count := atomic.AddInt32(&callCount, 1)
		c.JSON(http.StatusOK, gin.H{"host": c.Request.Host, "count": count})
	})

	reqA1 := httptest.NewRequest(http.MethodGet, "/tenant-data", nil)
	reqA1.Host = "tenant-a.example.com"
	wA1 := httptest.NewRecorder()
	r.ServeHTTP(wA1, reqA1)
	assert.Equal(t, http.StatusOK, wA1.Code)
	assert.Contains(t, wA1.Body.String(), `"host":"tenant-a.example.com"`)
	assert.Contains(t, wA1.Body.String(), `"count":1`)

	reqB1 := httptest.NewRequest(http.MethodGet, "/tenant-data", nil)
	reqB1.Host = "tenant-b.example.com"
	wB1 := httptest.NewRecorder()
	r.ServeHTTP(wB1, reqB1)
	assert.Equal(t, http.StatusOK, wB1.Code)
	assert.Contains(t, wB1.Body.String(), `"host":"tenant-b.example.com"`)
	assert.Contains(t, wB1.Body.String(), `"count":2`)

	reqA2 := httptest.NewRequest(http.MethodGet, "/tenant-data", nil)
	reqA2.Host = "tenant-a.example.com"
	wA2 := httptest.NewRecorder()
	r.ServeHTTP(wA2, reqA2)
	assert.Equal(t, http.StatusOK, wA2.Code)
	assert.Contains(t, wA2.Body.String(), `"host":"tenant-a.example.com"`)
	assert.Contains(t, wA2.Body.String(), `"count":1`)

	assert.True(t, cache.Has("GET|tenant-a.example.com|/tenant-data"))
	assert.True(t, cache.Has("GET|tenant-b.example.com|/tenant-data"))
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestCache_MultiValueHeadersRoundTripOnHit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	r.GET("/multi-headers", func(c *gin.Context) {
		c.Writer.Header().Add("Link", "</assets/a.css>; rel=preload")
		c.Writer.Header().Add("Link", "</assets/b.js>; rel=preload")
		c.Writer.Header().Add("Vary", "Accept-Encoding")
		c.Writer.Header().Add("Vary", "Origin")
		c.String(http.StatusOK, "ok")
	})

	req1 := httptest.NewRequest(http.MethodGet, "/multi-headers", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)
	require.ElementsMatch(t, []string{"</assets/a.css>; rel=preload", "</assets/b.js>; rel=preload"}, w1.Header().Values("Link"))
	require.ElementsMatch(t, []string{"Accept-Encoding", "Origin"}, w1.Header().Values("Vary"))

	req2 := httptest.NewRequest(http.MethodGet, "/multi-headers", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	require.ElementsMatch(t, []string{"</assets/a.css>; rel=preload", "</assets/b.js>; rel=preload"}, w2.Header().Values("Link"))
	require.ElementsMatch(t, []string{"Accept-Encoding", "Origin"}, w2.Header().Values("Vary"))
	assert.Equal(t, w1.Body.String(), w2.Body.String())
}

func TestCache_HeadersSetCorrectlyOnCacheHit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	r.GET("/headers-test", func(c *gin.Context) {
		c.Header("X-Custom-Header", "custom-value")
		c.Header("Content-Type", "application/json")
		c.Header("Cache-Control", "max-age=3600")
		c.JSON(200, gin.H{"message": "test"})
	})

	// First request - populates cache
	req1 := httptest.NewRequest("GET", "/headers-test", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, 200, w1.Code)
	assert.Equal(t, "custom-value", w1.Header().Get("X-Custom-Header"))
	assert.Equal(t, "application/json", w1.Header().Get("Content-Type"))
	assert.Equal(t, "max-age=3600", w1.Header().Get("Cache-Control"))

	// Second request - should hit cache and return same headers
	req2 := httptest.NewRequest("GET", "/headers-test", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 200, w2.Code)
	assert.Equal(t, "custom-value", w2.Header().Get("X-Custom-Header"))
	assert.Equal(t, "application/json", w2.Header().Get("Content-Type"))
	assert.Equal(t, "max-age=3600", w2.Header().Get("Cache-Control"))
	assert.Equal(t, w1.Body.String(), w2.Body.String())
}

func TestCache_SkipAuthenticatedRequestsByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	var callCount int32
	r.GET("/profile", func(c *gin.Context) {
		count := atomic.AddInt32(&callCount, 1)
		c.JSON(200, gin.H{"count": count})
	})

	req1 := httptest.NewRequest("GET", "/profile", nil)
	req1.Header.Set("Authorization", "Bearer user-token-a")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)
	assert.Contains(t, w1.Body.String(), `"count":1`)

	req2 := httptest.NewRequest("GET", "/profile", nil)
	req2.Header.Set("Authorization", "Bearer user-token-b")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), `"count":2`)

	assert.False(t, cache.Has(defaultTestCacheKey("GET", "/profile")), "Authenticated requests should not be cached by default")
}

func TestCache_SkipCookieAuthenticatedRequestsByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	var callCount int32
	r.GET("/profile", func(c *gin.Context) {
		count := atomic.AddInt32(&callCount, 1)
		c.JSON(200, gin.H{"count": count})
	})

	req1 := httptest.NewRequest("GET", "/profile", nil)
	req1.Header.Set("Cookie", "session=user-a")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)
	assert.Contains(t, w1.Body.String(), `"count":1`)

	req2 := httptest.NewRequest("GET", "/profile", nil)
	req2.Header.Set("Cookie", "session=user-b")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), `"count":2`)

	assert.False(t, cache.Has(defaultTestCacheKey("GET", "/profile")), "Cookie-authenticated requests should not be cached by default")
}

func TestCache_RespectsHTTPSemantics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		setupHandler   func(c *gin.Context)
		shouldBeCached bool
		description    string
	}{
		{
			name: "should_not_cache_no_store",
			setupHandler: func(c *gin.Context) {
				c.Header("Cache-Control", "no-store")
				c.JSON(200, gin.H{"message": "not cacheable"})
			},
			shouldBeCached: false,
			description:    "Responses with Cache-Control: no-store should not be cached",
		},
		{
			name: "should_not_cache_private",
			setupHandler: func(c *gin.Context) {
				c.Header("Cache-Control", "private, max-age=3600")
				c.JSON(200, gin.H{"message": "private data"})
			},
			shouldBeCached: false,
			description:    "Responses with Cache-Control: private should not be cached",
		},
		{
			name: "should_not_cache_no_cache",
			setupHandler: func(c *gin.Context) {
				c.Header("Cache-Control", "no-cache")
				c.JSON(200, gin.H{"message": "requires revalidation"})
			},
			shouldBeCached: false,
			description:    "Responses with Cache-Control: no-cache should not be cached",
		},
		{
			name: "should_not_cache_must_revalidate_with_max_age_zero",
			setupHandler: func(c *gin.Context) {
				c.Header("Cache-Control", "max-age=0, must-revalidate")
				c.JSON(200, gin.H{"message": "must revalidate"})
			},
			shouldBeCached: false,
			description:    "Responses with max-age=0 and must-revalidate should not be cached",
		},
		{
			name: "should_not_cache_with_set_cookie",
			setupHandler: func(c *gin.Context) {
				c.SetCookie("session", "abc123", 3600, "/", "", false, true)
				c.JSON(200, gin.H{"message": "user session"})
			},
			shouldBeCached: false,
			description:    "Responses with Set-Cookie should not be cached",
		},
		{
			name: "should_cache_normal_response",
			setupHandler: func(c *gin.Context) {
				c.Header("Cache-Control", "public, max-age=3600")
				c.JSON(200, gin.H{"message": "cacheable"})
			},
			shouldBeCached: true,
			description:    "Normal responses should be cached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := shardedcache.NewCache(shardedcache.Options{
				MaxSize:           100,
				DefaultExpiration: time.Minute,
				ShardCount:        4,
			})

			r := gin.New()
			r.Use(NewChain().Use(Cache(cache)).Build())

			r.GET("/test", tt.setupHandler)

			// First request
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, 200, w.Code, tt.description)

			// Check if response was cached
			if tt.shouldBeCached {
				assert.True(t, cache.Has(defaultTestCacheKey("GET", "/test")), "Response should be cached: %s", tt.description)
			} else {
				assert.False(t, cache.Has(defaultTestCacheKey("GET", "/test")), "Response should not be cached: %s", tt.description)
			}
		})
	}
}

func TestCache_RangeRequestDoesNotPolluteNormalGETCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cache := shardedcache.NewCache(shardedcache.Options{
		MaxSize:           100,
		DefaultExpiration: time.Minute,
		ShardCount:        4,
	})

	r := gin.New()
	r.Use(NewChain().Use(Cache(cache)).Build())

	var callCount int32
	r.GET("/video", func(c *gin.Context) {
		count := atomic.AddInt32(&callCount, 1)
		if c.GetHeader("Range") != "" {
			c.Header("Content-Range", "bytes 0-3/10")
			c.String(http.StatusPartialContent, "part-%d", count)
			return
		}
		c.String(http.StatusOK, "full-%d", count)
	})

	rangeReq := httptest.NewRequest(http.MethodGet, "/video", nil)
	rangeReq.Header.Set("Range", "bytes=0-3")
	rangeW := httptest.NewRecorder()
	r.ServeHTTP(rangeW, rangeReq)

	assert.Equal(t, http.StatusPartialContent, rangeW.Code)
	assert.Equal(t, "part-1", rangeW.Body.String())
	assert.False(t, cache.Has(defaultTestCacheKey(http.MethodGet, "/video")), "Range partial response must not be cached")

	normalReq := httptest.NewRequest(http.MethodGet, "/video", nil)
	normalW := httptest.NewRecorder()
	r.ServeHTTP(normalW, normalReq)

	assert.Equal(t, http.StatusOK, normalW.Code)
	assert.Equal(t, "full-2", normalW.Body.String())
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount), "normal GET should not hit a cached partial response")
	assert.True(t, cache.Has(defaultTestCacheKey(http.MethodGet, "/video")), "normal GET response should be cached")
}
