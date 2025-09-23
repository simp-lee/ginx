package ginx

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequestIDConfig holds configuration for the RequestID middleware
type RequestIDConfig struct {
	// Header is the request/response header name to carry the ID
	// Common choices: "X-Request-ID" (default) or "Traceparent" in W3C Trace Context
	Header string

	// Generator generates a new ID when the incoming request doesn't have one
	Generator func() string

	// RespectIncoming controls whether to trust and reuse the incoming header value
	// If false, always override with a new ID
	RespectIncoming bool
}

// RequestID options
type RequestIDOption = Option[RequestIDConfig]

// WithRequestIDHeader sets the header name (default: X-Request-ID)
func WithRequestIDHeader(header string) RequestIDOption {
	return func(c *RequestIDConfig) { c.Header = header }
}

// WithRequestIDGenerator sets a custom ID generator
func WithRequestIDGenerator(gen func() string) RequestIDOption {
	return func(c *RequestIDConfig) { c.Generator = gen }
}

// WithIgnoreIncoming disables using incoming header value; always generate a new ID
func WithIgnoreIncoming() RequestIDOption {
	return func(c *RequestIDConfig) { c.RespectIncoming = false }
}

// RequestID provides a simple request ID middleware.
// Behavior:
// - Read ID from Header (default: X-Request-ID) if present and RespectIncoming=true
// - Otherwise generate a new ID using crypto/rand (16 bytes -> 32 hex chars)
// - Store into gin context via SetRequestID and echo back in response header
func RequestID(opts ...RequestIDOption) Middleware {
	cfg := RequestIDConfig{
		Header:          "X-Request-ID",
		Generator:       defaultRequestID,
		RespectIncoming: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Defensive defaults and normalization
	if cfg.Header == "" {
		cfg.Header = "X-Request-ID"
	}
	// Keep original header casing as provided by user to respect exact expectations
	if cfg.Generator == nil {
		cfg.Generator = defaultRequestID
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			id := ""
			if cfg.RespectIncoming {
				id = strings.TrimSpace(c.GetHeader(cfg.Header))
			}
			if id == "" {
				id = cfg.Generator()
			}

			// Set into context and response header early so downstream can use it
			SetRequestID(c, id)
			c.Writer.Header().Set(cfg.Header, id)

			// Also expose header for browsers when used with CORS
			// (non-breaking; if not using CORS it's harmless)
			// Avoid duplication if user already sets Access-Control-Expose-Headers elsewhere
			const exposeKey = "Access-Control-Expose-Headers"
			if existing := c.Writer.Header().Get(exposeKey); existing == "" {
				c.Writer.Header().Set(exposeKey, cfg.Header)
			} else if !headerValueContains(existing, cfg.Header) {
				c.Writer.Header().Set(exposeKey, existing+", "+cfg.Header)
			}

			next(c)
		}
	}
}

func defaultRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: use non-crypto random from request address/time is avoided to reduce deps; return short static when entropy fails
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// headerValueContains checks if a comma-separated header list contains a value (case-sensitive, simple check)
func headerValueContains(list, value string) bool {
	// Cheap parse without allocations for most small lists
	// Compare case-insensitively as header field-names are case-insensitive per RFC 7230
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	start := 0
	for i := 0; i <= len(list); i++ {
		if i == len(list) || list[i] == ',' {
			// trim spaces
			j := start
			for j < i && list[j] == ' ' {
				j++
			}
			k := i
			for k > j && list[k-1] == ' ' {
				k--
			}
			if j < k && strings.EqualFold(list[j:k], value) {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// Convenience condition: HasRequestID checks presence of request id in context
func HasRequestID() Condition {
	return func(c *gin.Context) bool {
		_, ok := GetRequestID(c)
		return ok
	}
}

// Expose helper to fetch from standard header if needed (not used by middleware chain directly)
func GetRequestIDFromHeader(r *http.Request, header string) string {
	if header == "" {
		header = "X-Request-ID"
	}
	return r.Header.Get(header)
}
