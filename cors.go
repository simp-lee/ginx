package ginx

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORSConfig CORS configuration structure
type CORSConfig struct {
	AllowOrigins     []string      // Allowed origins, defaults to same-origin
	AllowMethods     []string      // Allowed methods, defaults to GET, POST, PUT, DELETE, OPTIONS
	AllowHeaders     []string      // Allowed request headers, defaults to common headers
	ExposeHeaders    []string      // Headers exposed to the client
	AllowCredentials bool          // Whether to allow credentials, defaults to false
	MaxAge           time.Duration // Preflight request cache duration, defaults to 12 hours
}

// defaultCORSConfig provides default CORS configuration
func defaultCORSConfig() *CORSConfig {
	return &CORSConfig{
		AllowOrigins: []string{}, // default to no origins allowed, must be explicitly set
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowHeaders: []string{
			"Content-Type",
			"Authorization",
			"Cache-Control",
			"X-Requested-With",
		},
		ExposeHeaders:    []string{},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}
}

// WithAllowOrigins sets the allowed origins
func WithAllowOrigins(origins ...string) Option[CORSConfig] {
	return func(c *CORSConfig) {
		c.AllowOrigins = origins
	}
}

// WithAllowMethods sets the allowed methods
func WithAllowMethods(methods ...string) Option[CORSConfig] {
	return func(c *CORSConfig) {
		c.AllowMethods = methods
	}
}

// WithAllowHeaders sets the allowed request headers
func WithAllowHeaders(headers ...string) Option[CORSConfig] {
	return func(c *CORSConfig) {
		c.AllowHeaders = headers
	}
}

// WithExposeHeaders sets the exposed response headers
func WithExposeHeaders(headers ...string) Option[CORSConfig] {
	return func(c *CORSConfig) {
		c.ExposeHeaders = headers
	}
}

// WithAllowCredentials sets whether to allow credentials
func WithAllowCredentials(allow bool) Option[CORSConfig] {
	return func(c *CORSConfig) {
		c.AllowCredentials = allow
	}
}

// WithMaxAge sets the preflight request cache duration
func WithMaxAge(maxAge time.Duration) Option[CORSConfig] {
	return func(c *CORSConfig) {
		c.MaxAge = maxAge
	}
}

// CORS creates a CORS middleware (requires explicit origin configuration).
//
// NOTE: This function panics if AllowCredentials is true and AllowOrigins contains "*",
// as this is a security violation per the CORS specification. This follows the same
// pattern as regexp.MustCompile — configuration errors are caught at initialization
// time rather than producing silent security vulnerabilities at request time.
func CORS(options ...Option[CORSConfig]) Middleware {
	config := defaultCORSConfig()
	for _, option := range options {
		option(config)
	}

	// Security check: wildcard origin cannot be enabled with credentials
	if config.AllowCredentials && slices.Contains(config.AllowOrigins, "*") {
		panic("CORS security error: cannot use wildcard origin with credentials")
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			origin := c.Request.Header.Get("Origin")
			requestMethod := c.Request.Header.Get("Access-Control-Request-Method")

			// Handle preflight requests
			if isPreflightRequest(c.Request.Method, origin, requestMethod) {
				handlePreflight(c, config, origin)
				return
			}

			// Handle actual requests
			handleActualRequest(c, config, origin)
			next(c)
		}
	}
}

func isPreflightRequest(method, origin, requestMethod string) bool {
	return method == http.MethodOptions && origin != "" && requestMethod != ""
}

// CORSDefault creates a default CORS middleware (for development only)
func CORSDefault() Middleware {
	return CORS(WithAllowOrigins("*"))
}

// handlePreflight handles preflight requests
func handlePreflight(c *gin.Context, config *CORSConfig, origin string) {
	// Check if the origin is allowed
	if !isOriginAllowed(config.AllowOrigins, origin) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// Check if the request method is allowed
	requestMethod := c.Request.Header.Get("Access-Control-Request-Method")
	if requestMethod != "" && !slices.Contains(config.AllowMethods, requestMethod) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// Check if the request headers are allowed
	requestHeaders := c.Request.Header.Get("Access-Control-Request-Headers")
	if requestHeaders != "" && !areHeadersAllowed(config.AllowHeaders, requestHeaders) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// Set preflight-specific CORS response headers (includes Allow-Methods and Allow-Headers)
	setPreflightCORSHeaders(c, config, origin)

	// Set preflight request cache duration (only meaningful in preflight responses per CORS spec)
	if config.MaxAge > 0 {
		c.Header("Access-Control-Max-Age", strconv.Itoa(int(config.MaxAge.Seconds())))
	}

	// Set Vary headers for preflight requests to avoid proxy cache pollution
	setPreflightVaryHeaders(c)
	c.AbortWithStatus(http.StatusNoContent)
}

// handleActualRequest handles actual requests
func handleActualRequest(c *gin.Context, config *CORSConfig, origin string) {
	if isOriginAllowed(config.AllowOrigins, origin) {
		setActualCORSHeaders(c, config, origin)
	}
}

// setCORSOriginAndCredentials sets the common CORS headers shared between
// preflight and actual responses: Access-Control-Allow-Origin and
// Access-Control-Allow-Credentials.
func setCORSOriginAndCredentials(c *gin.Context, config *CORSConfig, origin string) {
	// Set allowed origin
	if slices.Contains(config.AllowOrigins, "*") {
		c.Header("Access-Control-Allow-Origin", "*")
	} else if origin != "" {
		c.Header("Access-Control-Allow-Origin", origin)
		addVaryHeaders(c, "Origin")
	}

	// Set whether to allow credentials
	if config.AllowCredentials {
		c.Header("Access-Control-Allow-Credentials", "true")
	}
}

// setPreflightCORSHeaders sets CORS response headers for preflight (OPTIONS) responses.
// Per the CORS specification, Access-Control-Allow-Methods and Access-Control-Allow-Headers
// are only meaningful in preflight responses.
func setPreflightCORSHeaders(c *gin.Context, config *CORSConfig, origin string) {
	setCORSOriginAndCredentials(c, config, origin)

	// Set allowed methods (preflight only per CORS spec)
	if len(config.AllowMethods) > 0 {
		c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ", "))
	}

	// Set allowed request headers (preflight only per CORS spec)
	if len(config.AllowHeaders) > 0 {
		c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ", "))
	}
}

// setActualCORSHeaders sets CORS response headers for actual (non-preflight) responses.
// Per the CORS specification, Access-Control-Expose-Headers is only meaningful in
// actual responses, while Access-Control-Allow-Methods/Headers are omitted.
func setActualCORSHeaders(c *gin.Context, config *CORSConfig, origin string) {
	setCORSOriginAndCredentials(c, config, origin)

	// Set exposed response headers (actual response only per CORS spec)
	if len(config.ExposeHeaders) > 0 {
		addExposeHeaders(c, config.ExposeHeaders...)
	}
}

// isOriginAllowed checks if the origin is allowed
func isOriginAllowed(allowedOrigins []string, origin string) bool {
	if len(allowedOrigins) == 0 {
		return false // Default: disallow all origins
	}
	return slices.Contains(allowedOrigins, "*") || slices.Contains(allowedOrigins, origin)
}

// areHeadersAllowed checks if the request headers are allowed
func areHeadersAllowed(allowedHeaders []string, requestHeaders string) bool {
	if requestHeaders == "" {
		return true
	}

	headers := strings.SplitSeq(requestHeaders, ",")
	for header := range headers {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		if !isHeaderAllowed(allowedHeaders, header) {
			return false
		}
	}
	return true
}

// isHeaderAllowed checks if a single request header is allowed
func isHeaderAllowed(allowedHeaders []string, header string) bool {
	header = strings.ToLower(header)
	return slices.ContainsFunc(allowedHeaders, func(allowed string) bool {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		return allowed == "*" || allowed == header
	})
}

// setPreflightVaryHeaders sets Vary headers for preflight requests to avoid proxy cache pollution
func setPreflightVaryHeaders(c *gin.Context) {
	// Set Vary headers to prevent incorrect caching of preflight responses
	// This ensures that different combinations of Origin, Method, and Headers
	// don't share the same cached response
	addVaryHeaders(c, "Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers")
}

func addVaryHeaders(c *gin.Context, values ...string) {
	existing := c.Writer.Header().Values("Vary")
	merged := make([]string, 0, len(existing)+len(values))
	seen := make(map[string]struct{}, len(existing)+len(values))

	add := func(v string) {
		for token := range strings.SplitSeq(v, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			key := strings.ToLower(token)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, token)
		}
	}

	for _, v := range existing {
		add(v)
	}
	for _, v := range values {
		add(v)
	}

	if len(merged) > 0 {
		c.Header("Vary", strings.Join(merged, ", "))
	}
}

func addExposeHeaders(c *gin.Context, values ...string) {
	existing := c.Writer.Header().Values("Access-Control-Expose-Headers")
	merged := make([]string, 0, len(existing)+len(values))
	seen := make(map[string]struct{}, len(existing)+len(values))

	add := func(v string) {
		for token := range strings.SplitSeq(v, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			key := strings.ToLower(token)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, token)
		}
	}

	for _, v := range existing {
		add(v)
	}
	for _, v := range values {
		add(v)
	}

	if len(merged) > 0 {
		c.Header("Access-Control-Expose-Headers", strings.Join(merged, ", "))
	}
}
