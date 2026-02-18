package ginx

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// requestIDRandRead is a package-level variable to allow test injection of a failing
// rand.Read implementation. It is only mutated in tests (never in production code).
// Test files that reassign this variable must not run in parallel with other tests
// that depend on request ID generation.
var requestIDRandRead = rand.Read

// requestIDFallbackCounter is atomically incremented as a fallback when crypto/rand fails.
var requestIDFallbackCounter uint64

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
			// Uses the shared addExposeHeaders helper from cors.go for deduplication
			addExposeHeaders(c, cfg.Header)

			next(c)
		}
	}
}

func defaultRequestID() string {
	var b [16]byte
	if _, err := requestIDRandRead(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[0:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(b[8:16], atomic.AddUint64(&requestIDFallbackCounter, 1))
	}
	return hex.EncodeToString(b[:])
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
