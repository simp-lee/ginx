package ginx

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// bufferedWriter buffers response content before timeout occurs
type bufferedWriter struct {
	gin.ResponseWriter
	body          *bytes.Buffer
	headers       http.Header
	statusCode    int
	statusSet     bool
	mutex         sync.RWMutex
	timedOut      atomic.Bool
	written       bool
	maxBufferSize int // Maximum buffer size in bytes (0 = unlimited)
}

func newBufferedWriter(w gin.ResponseWriter, maxBufferSize int) *bufferedWriter {
	return &bufferedWriter{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
		headers:        make(http.Header),
		statusCode:     200,
		statusSet:      false,
		maxBufferSize:  maxBufferSize,
	}
}

func (w *bufferedWriter) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.timedOut.Load() {
		// If already timed out, ignore the write
		return len(data), nil
	}

	// After buffer overflow flush, continue writing directly to the real writer.
	// Without this, subsequent small writes would go to w.body but never be flushed
	// (flushToReal skips when w.written is true), causing silent data loss.
	if w.written {
		return w.ResponseWriter.Write(data)
	}

	// If max buffer size is configured and would be exceeded, flush buffered content
	// directly to the underlying writer, bypassing timeout protection for this
	// oversized response. This prevents memory exhaustion from very large response bodies.
	if w.maxBufferSize > 0 && w.body.Len()+len(data) > w.maxBufferSize {
		w.copyHeaders()
		w.ResponseWriter.WriteHeader(w.statusCode)
		if w.body.Len() > 0 {
			if _, err := w.ResponseWriter.Write(w.body.Bytes()); err != nil {
				_, _ = fmt.Fprintf(gin.DefaultErrorWriter, "[ginx][timeout] failed to flush buffered content on overflow: %v\n", err)
			}
			w.body.Reset()
		}
		w.written = true
		return w.ResponseWriter.Write(data)
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
	w.statusSet = true
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
		if _, err := w.ResponseWriter.Write(w.body.Bytes()); err != nil {
			_, _ = fmt.Fprintf(gin.DefaultErrorWriter, "[ginx][timeout] failed to flush buffered response: %v\n", err)
		}
	}
	w.written = true
}

// adoptStatusFromOriginal syncs status code from the original writer when no explicit status was set.
func (w *bufferedWriter) adoptStatusFromOriginal(original gin.ResponseWriter) {
	if original == nil {
		return
	}

	status := original.Status()
	if status <= 0 {
		return
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.timedOut.Load() || w.written || w.statusSet {
		return
	}

	w.statusCode = status
	w.statusSet = true
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
