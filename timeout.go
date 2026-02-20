package ginx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// TimeoutConfig timeout middleware configuration
type TimeoutConfig struct {
	Timeout time.Duration `json:"timeout"` // Timeout duration
	// MaxBufferSize limits the response buffer size in bytes (0 = unlimited).
	// When a response exceeds this limit, it is flushed directly to the client,
	// bypassing timeout protection for that request. This prevents memory exhaustion
	// from very large response bodies while preserving timeout behavior for normal responses.
	MaxBufferSize int `json:"max_buffer_size"`
}

// defaultTimeoutConfig returns default timeout configuration
func defaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		Timeout: 30 * time.Second,
	}
}

// WithTimeout sets timeout duration
func WithTimeout(timeout time.Duration) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.Timeout = timeout
	}
}

// WithMaxBufferSize sets the maximum response buffer size in bytes.
// When a handler writes a response larger than this limit, the buffered content
// is flushed directly to the client, bypassing timeout protection for that request.
// This prevents memory exhaustion from very large response bodies (e.g., file downloads
// or large database dumps). A value of 0 (default) means unlimited buffering.
func WithMaxBufferSize(size int) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.MaxBufferSize = size
	}
}

// writeTimeoutResponse writes timeout response.
// It acquires bufferedWriter.mutex to serialize against the handler goroutine's
// buffer-overflow path (which writes directly to originalWriter under the same mutex).
// If the handler has already flushed to the real writer (written == true), the timeout
// response is skipped because the client is already receiving the handler's response.
func writeTimeoutResponse(originalWriter gin.ResponseWriter, bufferedWriter *bufferedWriter, formatter ErrorFormatter) {
	bufferedWriter.mutex.Lock()
	bufferedWriter.markTimeout()

	// If handler already flushed to real writer (buffer overflow or hijack),
	// we've lost exclusive access to the response stream. Skip writing timeout response.
	if bufferedWriter.written {
		bufferedWriter.mutex.Unlock()
		return
	}
	bufferedWriter.written = true
	bufferedWriter.mutex.Unlock()

	// Now safe: handler goroutine will see written=true && timedOut=true and won't
	// attempt further writes to originalWriter.
	clearTimeoutEntityHeaders(originalWriter.Header())

	// Set timeout-specific headers (may override existing Content-Type)
	originalWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	originalWriter.Header().Set("X-Timeout", "true")
	originalWriter.WriteHeader(http.StatusRequestTimeout)

	// Build response body via formatter or default
	var body any
	if formatter != nil {
		body = formatter(http.StatusRequestTimeout, "request timeout")
	} else {
		body = gin.H{"error": "request timeout"}
	}

	jsonBytes, err := json.Marshal(body)
	if err != nil {
		jsonBytes = []byte(`{"error":"request timeout"}`)
	}

	if _, err := originalWriter.Write(jsonBytes); err != nil {
		_, _ = fmt.Fprintf(gin.DefaultErrorWriter, "[ginx][timeout] failed to write timeout response: %v\n", err)
	}

	bufferedWriter.mutex.Lock()
	bufferedWriter.statusCode = http.StatusRequestTimeout
	bufferedWriter.statusSet = true
	bufferedWriter.body.Reset()
	bufferedWriter.body.Write(jsonBytes)
	bufferedWriter.mutex.Unlock()
}

func clearTimeoutEntityHeaders(headers http.Header) {
	if headers == nil {
		return
	}

	headers.Del("Content-Length")
	headers.Del("Content-Encoding")
	headers.Del("Trailer")
	headers.Del("Transfer-Encoding")
}

func reportPanicAfterTimeout(p any) {
	_, _ = fmt.Fprintf(gin.DefaultErrorWriter, "[ginx][timeout] panic after deadline: %v\n", p)
}

func observePanicAfterTimeout(done <-chan struct{}, panicChan <-chan any) {
	select {
	case <-done:
		select {
		case p := <-panicChan:
			reportPanicAfterTimeout(p)
		default:
		}
	default:
		go func() {
			<-done
			select {
			case p := <-panicChan:
				reportPanicAfterTimeout(p)
			default:
			}
		}()
	}
}

// Timeout middleware to set a timeout for requests.
// This version executes downstream handlers on an isolated context copy to avoid sharing
// mutable request execution state between goroutines.
func Timeout(options ...Option[TimeoutConfig]) Middleware {
	config := defaultTimeoutConfig()
	for _, option := range options {
		option(config)
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			formatter := GetErrorFormatter(c)

			if config.Timeout <= 0 {
				c.Header("X-Timeout", "true")
				AbortWithError(c, http.StatusRequestTimeout, "request timeout")
				return
			}

			originalWriter := c.Writer
			bufferedWriter := newBufferedWriter(originalWriter, config.MaxBufferSize)

			ctxWithTimeout, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
			defer cancel()
			execRequest := c.Request.WithContext(ctxWithTimeout)
			execContext := cloneContextForTimeout(c, bufferedWriter, execRequest)

			done := make(chan struct{})
			panicChan := make(chan any, 1)

			go func() {
				defer close(done)
				defer func() {
					if r := recover(); r != nil {
						panicChan <- r
					}
				}()
				next(execContext)
			}()

			select {
			case <-done:
				select {
				case r := <-panicChan:
					panic(r)
				default:
				}

				if ctxWithTimeout.Err() == context.DeadlineExceeded {
					bufferedWriter.copyHeaders()
					writeTimeoutResponse(originalWriter, bufferedWriter, formatter)
					c.Abort()
					return
				}

				syncContextFromTimeoutExecution(c, execContext)
				bufferedWriter.adoptStatusFromOriginal(originalWriter)
				bufferedWriter.flushToReal()
				c.Abort()
			case <-ctxWithTimeout.Done():
				if ctxWithTimeout.Err() == context.DeadlineExceeded {
					// Copy any buffered headers if handler already finished (non-blocking).
					// In production HTTP, headers must be set BEFORE the first Write call,
					// so we copy them before writeTimeoutResponse writes the status+body.
					select {
					case <-done:
						bufferedWriter.copyHeaders()
					default:
					}
					writeTimeoutResponse(originalWriter, bufferedWriter, formatter)
					observePanicAfterTimeout(done, panicChan)
				}
				c.Abort()
			}
		}
	}
}

// IsTimeout checks if the current request has timed out.
// Returns true if the request was terminated due to timeout.
//
// Note: This function reads the X-Timeout header from c.Writer, which is the
// original response writer in outer middleware. Inside a timeout-protected handler,
// c.Writer is a buffered writer and the X-Timeout header may not be visible.
// Handlers should use c.Request.Context().Done() to detect their own timeout.
func IsTimeout(c *gin.Context) bool {
	return c.Writer.Header().Get("X-Timeout") == "true"
}
