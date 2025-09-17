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

// CORS creates a CORS middleware (requires explicit origin configuration)
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

			// Handle preflight requests
			if c.Request.Method == http.MethodOptions {
				handlePreflight(c, config, origin)
				return
			}

			// Handle actual requests
			handleActualRequest(c, config, origin)
			next(c)
		}
	}
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

	// Set CORS response headers
	setCORSHeaders(c, config, origin)

	// Set Vary headers for preflight requests to avoid proxy cache pollution
	setPreflightVaryHeaders(c)
	c.AbortWithStatus(http.StatusNoContent)
}

// handleActualRequest handles actual requests
func handleActualRequest(c *gin.Context, config *CORSConfig, origin string) {
	if isOriginAllowed(config.AllowOrigins, origin) {
		setCORSHeaders(c, config, origin)
	}
}

// setCORSHeaders sets CORS response headers
func setCORSHeaders(c *gin.Context, config *CORSConfig, origin string) {
	// Set allowed origin
	if slices.Contains(config.AllowOrigins, "*") {
		c.Header("Access-Control-Allow-Origin", "*")
	} else if origin != "" {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
	}

	// Set allowed methods
	if len(config.AllowMethods) > 0 {
		c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ", "))
	}

	// Set allowed request headers
	if len(config.AllowHeaders) > 0 {
		c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ", "))
	}

	// Set exposed response headers
	if len(config.ExposeHeaders) > 0 {
		c.Header("Access-Control-Expose-Headers", strings.Join(config.ExposeHeaders, ", "))
	}

	// Set whether to allow credentials
	if config.AllowCredentials {
		c.Header("Access-Control-Allow-Credentials", "true")
	}

	// Set preflight request cache duration
	if config.MaxAge > 0 {
		c.Header("Access-Control-Max-Age", strconv.Itoa(int(config.MaxAge.Seconds())))
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
	headers := strings.SplitSeq(requestHeaders, ",")
	for header := range headers {
		header = strings.TrimSpace(header)
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
		return strings.ToLower(allowed) == header
	})
}

// setPreflightVaryHeaders sets Vary headers for preflight requests to avoid proxy cache pollution
func setPreflightVaryHeaders(c *gin.Context) {
	// Set Vary headers to prevent incorrect caching of preflight responses
	// This ensures that different combinations of Origin, Method, and Headers
	// don't share the same cached response
	c.Header("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
}
