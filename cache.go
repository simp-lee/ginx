package ginx

import (
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	shardedcache "github.com/simp-lee/cache"
)

// CacheKeyFunc generates cache key for request context.
type CacheKeyFunc func(*gin.Context) string

// CacheOption configures cache middleware behavior.
type CacheOption func(*cacheOptions)

type cacheOptions struct {
	keyFunc     CacheKeyFunc
	varyHeaders []string
}

func defaultCacheOptions() cacheOptions {
	return cacheOptions{
		varyHeaders: []string{"Accept-Encoding"},
	}
}

func buildCacheOptions(opts ...CacheOption) cacheOptions {
	options := defaultCacheOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	options.varyHeaders = normalizeCacheVaryHeaders(options.varyHeaders)
	if options.keyFunc == nil {
		headers := append([]string(nil), options.varyHeaders...)
		options.keyFunc = func(c *gin.Context) string {
			return generateCacheKeyWithVary(c, headers)
		}
	}
	return options
}

// WithCacheKeyFunc sets custom cache key generation strategy.
func WithCacheKeyFunc(fn CacheKeyFunc) CacheOption {
	return func(options *cacheOptions) {
		if fn != nil {
			options.keyFunc = fn
		}
	}
}

// WithCacheVaryHeaders configures request headers that should participate in default cache key generation.
// Defaults to Accept-Encoding for safe content-encoding separation.
func WithCacheVaryHeaders(headers ...string) CacheOption {
	return func(options *cacheOptions) {
		options.varyHeaders = append([]string(nil), headers...)
	}
}

// cachedResponse represents a cached HTTP response
type cachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
}

// Cache creates a cache middleware using the default cache group
func Cache(cache shardedcache.CacheInterface) Middleware {
	if cache == nil {
		panic("cache middleware requires non-nil cache backend")
	}

	return cacheWithGroup(cache, "", buildCacheOptions())
}

// CacheWithOptions creates a cache middleware using default group and custom options.
func CacheWithOptions(cache shardedcache.CacheInterface, opts ...CacheOption) Middleware {
	if cache == nil {
		panic("cache middleware requires non-nil cache backend")
	}

	return cacheWithGroup(cache, "", buildCacheOptions(opts...))
}

// CacheWithGroup creates a cache middleware using the specified cache group
func CacheWithGroup(cache shardedcache.CacheInterface, groupName string) Middleware {
	if cache == nil {
		panic("cache middleware requires non-nil cache backend")
	}

	return cacheWithGroup(cache, groupName, buildCacheOptions())
}

// CacheWithGroupOptions creates a cache middleware using specified group and custom options.
func CacheWithGroupOptions(cache shardedcache.CacheInterface, groupName string, opts ...CacheOption) Middleware {
	if cache == nil {
		panic("cache middleware requires non-nil cache backend")
	}

	return cacheWithGroup(cache, groupName, buildCacheOptions(opts...))
}

// cacheWithGroup provides the internal cache middleware implementation
func cacheWithGroup(cache shardedcache.CacheInterface, groupName string, options cacheOptions) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			// Only cache GET and HEAD requests; pass through other methods
			if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
				next(c)
				return
			}

			// Range requests must bypass cache to avoid serving partial content for normal GET requests.
			if c.GetHeader("Range") != "" {
				next(c)
				return
			}

			// Skip caching authenticated/session requests by default to avoid cross-user data leakage.
			if c.GetHeader("Authorization") != "" || c.GetHeader("Cookie") != "" {
				next(c)
				return
			}

			key := options.keyFunc(c)

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
				for k, values := range response.Headers {
					for _, value := range values {
						c.Writer.Header().Add(k, value)
					}
				}
				// Ensure Vary header is set for proper HTTP caching semantics,
				// even if the cached response predates this feature
				if len(options.varyHeaders) > 0 {
					ensureVaryHeaders(c.Writer.Header(), options.varyHeaders)
				}
				c.Writer.WriteHeader(response.StatusCode)
				if c.Request.Method != http.MethodHead {
					if _, err := c.Writer.Write(response.Body); err != nil {
						_ = c.Error(err)
					}
				}
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

			// Ensure Vary headers are present after handler runs, adding only values
			// not already set by the handler to avoid duplicates.
			if len(options.varyHeaders) > 0 {
				ensureVaryHeaders(c.Writer.Header(), options.varyHeaders)
			}

			// Cache response after request processing if not cached yet and status code is valid.
			// 206 Partial Content must never be cached with the default key because it has Range semantics.
			if writer.Status() >= 200 && writer.Status() < 300 && writer.Status() != http.StatusPartialContent {
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
	writeErr  bool // tracks if any write error occurred during response
}

func (w *responseWriter) Write(data []byte) (int, error) {
	ret, err := w.ResponseWriter.Write(data)
	if err != nil {
		w.writeErr = true
	}
	if ret > 0 {
		w.body = append(w.body, data[:ret]...)
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

	// Skip caching if any write error occurred to avoid serving truncated/corrupted responses
	if w.writeErr {
		return
	}

	// Check Cache-Control directives - respect HTTP semantics
	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "" {
		directives := parseCacheControlDirectives(cacheControl)

		// Don't cache responses with directives requiring private handling or revalidation
		if hasDirective(directives, "no-store") ||
			hasDirective(directives, "private") ||
			hasDirective(directives, "no-cache") ||
			hasDirective(directives, "must-revalidate") ||
			isZeroMaxAge(directives) {
			return
		}
	}

	// Don't cache responses with Set-Cookie header to avoid user-specific data leakage
	if w.Header().Get("Set-Cookie") != "" {
		return
	}

	// Partial/ranged responses should not be cached, even if status is not 206.
	if w.Header().Get("Content-Range") != "" {
		return
	}

	// Copy response headers
	headers := make(map[string][]string, len(w.Header()))
	for k, v := range w.Header() {
		if len(v) > 0 {
			headers[k] = append([]string(nil), v...)
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

// ensureVaryHeaders adds Vary header values that are not already present, avoiding duplicates
// when the handler has already set some of the same Vary values.
func ensureVaryHeaders(h http.Header, varyHeaders []string) {
	existing := h.Values("Vary")
	// Build a set of already-present values (case-insensitive)
	seen := make(map[string]struct{}, len(existing))
	for _, v := range existing {
		// Each value may be comma-separated (e.g., "Accept-Encoding, Origin")
		for _, part := range strings.Split(v, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				seen[strings.ToLower(trimmed)] = struct{}{}
			}
		}
	}
	for _, header := range varyHeaders {
		if _, ok := seen[strings.ToLower(header)]; !ok {
			h.Add("Vary", header)
		}
	}
}

// generateCacheKey creates a cache key using default Vary headers (Accept-Encoding).
func generateCacheKey(c *gin.Context) string {
	return generateCacheKeyWithVary(c, []string{"Accept-Encoding"})
}

func generateCacheKeyWithVary(c *gin.Context, varyHeaders []string) string {
	host := strings.ToLower(c.Request.Host)
	method := c.Request.Method
	path := c.Request.URL.Path
	query := c.Request.URL.RawQuery
	headers := normalizeCacheVaryHeaders(varyHeaders)

	capacity := len(method) + 1 + len(host) + 1 + len(path)
	if query != "" {
		capacity += 1 + len(query)
	}
	for _, headerName := range headers {
		value := c.GetHeader(headerName)
		if value == "" {
			continue
		}
		capacity += 4 + len(headerName) + len(value)
	}

	var builder strings.Builder
	builder.Grow(capacity)
	builder.WriteString(method)
	builder.WriteByte('|')
	builder.WriteString(host)
	builder.WriteByte('|')
	builder.WriteString(path)
	if query != "" {
		builder.WriteByte('?')
		builder.WriteString(query)
	}
	for _, headerName := range headers {
		value := c.GetHeader(headerName)
		if value == "" {
			continue
		}
		builder.WriteString("|h:")
		builder.WriteString(headerName)
		builder.WriteByte('=')
		builder.WriteString(value)
	}
	return builder.String()
}

func normalizeCacheVaryHeaders(headers []string) []string {
	if len(headers) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(headers))
	seen := make(map[string]struct{}, len(headers))

	for _, header := range headers {
		trimmed := strings.TrimSpace(header)
		if trimmed == "" {
			continue
		}

		canonical := textproto.CanonicalMIMEHeaderKey(trimmed)
		lookupKey := strings.ToLower(canonical)
		if _, exists := seen[lookupKey]; exists {
			continue
		}

		seen[lookupKey] = struct{}{}
		normalized = append(normalized, canonical)
	}

	return normalized
}

func parseCacheControlDirectives(cacheControl string) map[string]string {
	parts := strings.Split(cacheControl, ",")
	directives := make(map[string]string, len(parts))

	for _, part := range parts {
		token := strings.TrimSpace(strings.ToLower(part))
		if token == "" {
			continue
		}
		if idx := strings.IndexByte(token, '='); idx >= 0 {
			name := strings.TrimSpace(token[:idx])
			value := strings.Trim(strings.TrimSpace(token[idx+1:]), "\"")
			if name != "" {
				directives[name] = value
			}
			continue
		}
		directives[token] = ""
	}

	return directives
}

func hasDirective(directives map[string]string, name string) bool {
	_, exists := directives[name]
	return exists
}

func isZeroMaxAge(directives map[string]string) bool {
	v, exists := directives["max-age"]
	if !exists {
		return false
	}
	age, err := strconv.Atoi(v)
	if err != nil {
		return false
	}
	return age == 0
}
