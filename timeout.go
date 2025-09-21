package ginx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// bufferedWriter buffers response content before timeout occurs
type bufferedWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	headers    http.Header
	statusCode int
	mutex      sync.RWMutex
	timedOut   atomic.Bool
	written    bool
}

func newBufferedWriter(w gin.ResponseWriter) *bufferedWriter {
	return &bufferedWriter{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
		headers:        make(http.Header),
		statusCode:     200,
	}
}

func (w *bufferedWriter) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.timedOut.Load() {
		// If already timed out, ignore the write
		return len(data), nil
	}

	return w.body.Write(data)
}

func (w *bufferedWriter) WriteHeader(statusCode int) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.timedOut.Load() || w.written {
		return
	}

	w.statusCode = statusCode
}

func (w *bufferedWriter) Header() http.Header {
	// Fully buffered header: always return buffered headers until flushToReal copies them uniformly
	return w.headers
}

func (w *bufferedWriter) WriteHeaderNow() {
	// In buffered mode, writing real response early would break subsequent timeout
	// judgment and consistency, so treat this as no-op. Only flushToReal writes
	// out uniformly at the final stage. This prevents early response even if
	// business code explicitly calls WriteHeaderNow.
}

func (w *bufferedWriter) Size() int {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.body.Len()
}

func (w *bufferedWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *bufferedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// Use write lock to ensure mutual exclusion with other operations
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// Disallow Hijack after timeout to avoid inconsistent connection state
	if w.timedOut.Load() {
		return nil, nil, http.ErrNotSupported
	}

	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		// Safely attempt Hijack, capture potential panic
		var conn net.Conn
		var rw *bufio.ReadWriter
		var err error

		func() {
			defer func() {
				if r := recover(); r != nil {
					// If underlying implementation panics (e.g., test environment), convert to error
					err = http.ErrNotSupported
				}
			}()
			conn, rw, err = hijacker.Hijack()
		}()

		// Only mark as written when Hijack succeeds
		if err == nil {
			w.written = true
		}

		return conn, rw, err
	}
	return nil, nil, http.ErrNotSupported
}

func (w *bufferedWriter) Flush() {
	// Use write lock to ensure mutual exclusion with Hijack and other operations
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// Only execute downstream Flush after buffered content has been flushed to real ResponseWriter,
	// avoiding race conditions caused by triggering underlying header/body sending during buffering stage.
	if w.written {
		if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// markTimeout marks as timed out state, preventing subsequent writes
func (w *bufferedWriter) markTimeout() {
	w.timedOut.Store(true)
}

// copyHeaders copies buffered headers to the real ResponseWriter
func (w *bufferedWriter) copyHeaders() {
	dst := w.ResponseWriter.Header()
	for key, values := range w.headers {
		// Use overwrite copy semantics (keeping final result closer to direct writing)
		// Create a copy of values to avoid sharing underlying array
		cp := make([]string, len(values))
		copy(cp, values)
		dst[key] = cp
	}
}

// flushToReal writes buffered content to the real ResponseWriter
func (w *bufferedWriter) flushToReal() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.timedOut.Load() || w.written {
		return
	}

	w.copyHeaders()
	w.ResponseWriter.WriteHeader(w.statusCode)
	if w.body.Len() > 0 {
		w.ResponseWriter.Write(w.body.Bytes())
	}
	w.written = true
}

// Status returns the buffered status code, allowing middleware in the chain to read the correct status
func (w *bufferedWriter) Status() int {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.statusCode
}

// Written returns the buffered write state, allowing middleware in the chain to read the correct write state
func (w *bufferedWriter) Written() bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.written
}

// Pusher passes through HTTP/2 Server Push functionality (if underlying support exists)
func (w *bufferedWriter) Pusher() http.Pusher {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher
	}
	return nil
}

// TimeoutConfig timeout middleware configuration
type TimeoutConfig struct {
	Timeout  time.Duration `json:"timeout"`  // Timeout duration
	Response any           `json:"response"` // Timeout response content
}

// defaultTimeoutConfig returns default timeout configuration
func defaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		Timeout: 30 * time.Second,
		Response: gin.H{
			"code":  408,
			"error": "request timeout",
		},
	}
}

// WithTimeout sets timeout duration
func WithTimeout(timeout time.Duration) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.Timeout = timeout
	}
}

// WithTimeoutResponse sets timeout response content
func WithTimeoutResponse(response any) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.Response = response
	}
}

// WithTimeoutMessage sets timeout message
func WithTimeoutMessage(message string) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.Response = gin.H{
			"error": message,
			"code":  408,
		}
	}
}

// writeTimeoutResponse writes timeout response
func writeTimeoutResponse(originalWriter gin.ResponseWriter, bufferedWriter *bufferedWriter, config *TimeoutConfig) {
	// Mark bufferedWriter as timed out
	bufferedWriter.markTimeout()

	// Copy buffered headers to preserve important headers (CORS, Trace-ID, etc.)
	// This is now safe since we're in the same goroutine - no race condition
	bufferedWriter.copyHeaders()

	// Set timeout-specific headers (may override existing Content-Type)
	originalWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	originalWriter.Header().Set("X-Timeout", "true")
	// Also set X-Timeout in bufferedWriter headers for IsTimeout function
	bufferedWriter.Header().Set("X-Timeout", "true")
	originalWriter.WriteHeader(http.StatusRequestTimeout)

	// Serialize JSON response directly from config or use default
	var jsonBytes []byte

	if config.Response != nil {
		if data, err := json.Marshal(config.Response); err == nil {
			jsonBytes = data
		} else {
			// Use default response as fallback if serialization fails
			jsonBytes = []byte(`{"code":408,"error":"request timeout"}`)
		}
	} else {
		jsonBytes = []byte(`{"code":408,"error":"request timeout"}`)
	}
	originalWriter.Write(jsonBytes)

	// Update buffered writer's visible state for downstream middleware
	// (such as logging) in the chain to read correct status code and response size
	// Since we're serial now, we can safely access without excessive locking
	bufferedWriter.mutex.Lock()
	bufferedWriter.statusCode = http.StatusRequestTimeout
	bufferedWriter.written = true
	// Clear and replace buffer with actual timeout response to ensure Size() accuracy
	bufferedWriter.body.Reset()
	bufferedWriter.body.Write(jsonBytes)
	bufferedWriter.mutex.Unlock()

}

// Timeout middleware to set a timeout for requests.
// This version uses a serial approach to avoid race conditions when accessing headers.
func Timeout(options ...Option[TimeoutConfig]) Middleware {
	config := defaultTimeoutConfig()
	for _, option := range options {
		option(config)
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			// Check for zero or negative timeout, immediately return timeout response
			if config.Timeout <= 0 {
				c.Header("Content-Type", "application/json; charset=utf-8")
				c.Header("X-Timeout", "true")
				c.AbortWithStatusJSON(http.StatusRequestTimeout, config.Response)
				return
			}

			// Save original Writer
			originalWriter := c.Writer

			// Create buffered writer
			bufferedWriter := newBufferedWriter(originalWriter)
			c.Writer = bufferedWriter

			// Create timeout context
			ctxWithTimeout, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
			defer cancel()
			// Set timeout context for original request, so copies also inherit it
			c.Request = c.Request.WithContext(ctxWithTimeout)

			// Execute handler directly in the same goroutine (serial execution)
			// This eliminates race conditions on shared header maps
			next(c)

			// Check if timeout occurred during execution
			contextTimedOut := ctxWithTimeout.Err() == context.DeadlineExceeded

			if contextTimedOut {
				// Timeout occurred - write timeout response
				// Since we're in the same goroutine, no race condition on headers
				writeTimeoutResponse(originalWriter, bufferedWriter, config)
			} else {
				// Handler completed within timeout, flush buffered content
				bufferedWriter.flushToReal()
			}
		}
	}
}

// IsTimeout checks if the current request has timed out.
// Returns true if the request was terminated due to timeout.
func IsTimeout(c *gin.Context) bool {
	return c.Writer.Header().Get("X-Timeout") == "true"
}
