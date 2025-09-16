package ginx

import (
	"strings"

	"github.com/gin-gonic/gin"
	shardedcache "github.com/simp-lee/cache"
)

// cachedResponse represents a cached HTTP response
type cachedResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
}

// Cache creates a cache middleware using the default cache group
func Cache(cache shardedcache.CacheInterface) Middleware {
	return cacheWithGroup(cache, "")
}

// CacheWithGroup creates a cache middleware using the specified cache group
func CacheWithGroup(cache shardedcache.CacheInterface, groupName string) Middleware {
	return cacheWithGroup(cache, groupName)
}

// cacheWithGroup provides the internal cache middleware implementation
func cacheWithGroup(cache shardedcache.CacheInterface, groupName string) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {

			key := generateCacheKey(c)

			// Choose caching strategy based on whether groups are used
			var response cachedResponse
			var exists bool

			if groupName != "" {
				group := cache.Group(groupName)
				if cached, found := group.Get(key); found {
					if resp, ok := cached.(cachedResponse); ok {
						response = resp
						exists = true
					}
				}
			} else {
				response, exists = shardedcache.GetTyped[cachedResponse](cache, key)
			}

			if exists {
				// Set headers first, then status code, then write body
				for k, v := range response.Headers {
					c.Writer.Header().Set(k, v)
				}
				c.Writer.WriteHeader(response.StatusCode)
				c.Writer.Write(response.Body)
				c.Abort()
				return
			}

			// Create response writer to capture response data
			writer := &responseWriter{
				ResponseWriter: c.Writer,
				cache:          cache,
				groupName:      groupName,
				key:            key,
				body:           make([]byte, 0),
			}
			c.Writer = writer

			next(c)

			// Cache response after request processing if not cached yet and status code is valid
			if writer.Status() >= 200 && writer.Status() < 300 {
				writer.cacheResponse()
			}
		}
	}
}

// responseWriter is a custom Gin response writer that captures response data for caching
type responseWriter struct {
	gin.ResponseWriter
	cache     shardedcache.CacheInterface
	groupName string
	key       string
	body      []byte
	cached    bool
}

func (w *responseWriter) Write(data []byte) (int, error) {
	ret, err := w.ResponseWriter.Write(data)
	if err == nil {
		// Accumulate response data
		w.body = append(w.body, data...)

		// Only cache successful responses
		if w.Status() >= 200 && w.Status() < 300 {
			w.cacheResponse()
		}
	}
	return ret, err
}

func (w *responseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *responseWriter) cacheResponse() {
	// Prevent duplicate caching
	if w.cached {
		return
	}
	w.cached = true

	// Check Cache-Control directives - respect HTTP semantics
	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "" {
		// Don't cache responses with no-store or private directives
		if contains(cacheControl, "no-store") || contains(cacheControl, "private") {
			return
		}
	}

	// Don't cache responses with Set-Cookie header to avoid user-specific data leakage
	if w.Header().Get("Set-Cookie") != "" {
		return
	}

	// Copy response headers
	headers := make(map[string]string)
	for k, v := range w.Header() {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	response := cachedResponse{
		StatusCode: w.Status(),
		Headers:    headers,
		Body:       w.body,
	}

	// Cache based on whether groups are used
	if w.groupName != "" {
		w.cache.Group(w.groupName).Set(w.key, response)
	} else {
		w.cache.Set(w.key, response)
	}
}

func generateCacheKey(c *gin.Context) string {
	if c.Request.URL.RawQuery != "" {
		return c.Request.URL.Path + "?" + c.Request.URL.RawQuery
	}
	return c.Request.URL.Path
}

// contains checks if a string contains any of the cache control directives
func contains(cacheControl, directive string) bool {
	// Simple case-insensitive substring check for cache control directives
	// In production, you might want more sophisticated parsing
	lower := strings.ToLower(cacheControl)
	return strings.Contains(lower, strings.ToLower(directive))
}
