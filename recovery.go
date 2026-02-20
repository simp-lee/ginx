package ginx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/logger"
)

type RecoveryHandler func(*gin.Context, any)

func defaultRecoveryHandler(c *gin.Context, err any) {
	AbortWithError(c, 500, "internal server error")
}

// Recovery creates a panic recovery middleware.
//
// NOTE: This function panics if the logger cannot be created (e.g., invalid file path).
// This follows the same pattern as regexp.MustCompile — configuration errors are caught
// at initialization time rather than at request time. Callers should ensure valid logger
// options are provided.
func Recovery(options ...logger.Option) Middleware {
	return RecoveryWith(nil, options...)
}

// RecoveryWith creates a panic recovery middleware with a custom handler.
//
// NOTE: This function panics if the logger cannot be created (e.g., invalid file path).
// This follows the same pattern as regexp.MustCompile — configuration errors are caught
// at initialization time rather than at request time. Callers should ensure valid logger
// options are provided.
func RecoveryWith(handler RecoveryHandler, loggerOptions ...logger.Option) Middleware {
	// Create logger instance
	log, err := logger.New(loggerOptions...)
	if err != nil {
		panic("failed to create logger for recovery: " + err.Error())
	}

	// Use default handler if not provided
	if handler == nil {
		handler = defaultRecoveryHandler
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			defer func() {
				if err := recover(); err != nil {
					// Check if the error is a broken pipe error
					brokenPipe := isBrokenPipe(err)

					// Log panic information
					if brokenPipe {
						// Only log basic information, do not log stack trace
						fields := []any{
							"error", fmt.Sprintf("%v", err),
							"path", c.Request.URL.Path,
							"method", c.Request.Method,
							"ip", c.ClientIP(),
						}
						if rid, ok := GetRequestID(c); ok && rid != "" {
							fields = append(fields, "request_id", rid)
						}
						log.Warn("Connection broken", fields...)
						// Write response is not possible when the connection is broken, so just abort
						if e, ok := err.(error); ok {
							c.Error(e)
						}
						c.Abort()
					} else {
						// Log full stack trace for actual panics
						stack := getStack()
						fields := []any{
							"error", fmt.Sprintf("%v", err),
							"path", c.Request.URL.Path,
							"method", c.Request.Method,
							"ip", c.ClientIP(),
							"user_agent", c.Request.UserAgent(),
							"stack", stack,
						}
						if rid, ok := GetRequestID(c); ok && rid != "" {
							fields = append(fields, "request_id", rid)
						}
						log.Error("Panic recovered", fields...)
						// Call recovery handler
						handler(c, err)
					}
				}
			}()

			// Execute the next middleware
			next(c)
		}
	}
}

// getStack retrieves the current stack trace information.
func getStack() string {
	// Start with 4096 bytes and grow until the full stack trace fits.
	// runtime.Stack returns the number of bytes written; if it equals len(buf),
	// the trace was likely truncated, so we double the buffer and retry.
	buf := make([]byte, 4096)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, len(buf)*2)
	}
	stack := string(buf)

	// Filter out stack frames related to the recovery middleware
	lines := strings.Split(stack, "\n")
	var filteredLines []string
	skipNext := false

	for _, line := range lines {
		// Skip lines containing recovery.go and the next line (file location)
		if strings.Contains(line, "recovery.go") {
			skipNext = true
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		// Skip lines related to runtime panic
		if strings.Contains(line, "runtime.gopanic") ||
			strings.Contains(line, "runtime/panic.go") {
			continue
		}

		// Clean function name (borrowed from Gin)
		line = cleanFunctionName(line)
		filteredLines = append(filteredLines, line)
	}

	return strings.Join(filteredLines, "\n")
}

// cleanFunctionName cleans up the function name by removing the package path and fixing special characters.
func cleanFunctionName(line string) string {
	// Heuristic: lines containing both "/" and ":" are file path lines (e.g., "/app/main.go:42").
	// Function name lines may contain "/" (package paths) but rarely contain ":" without a file path.
	// This heuristic is intentionally conservative — it may skip cleaning some edge-case function
	// names, but it avoids corrupting file path references in stack traces.
	if strings.Contains(line, "/") && strings.Contains(line, ":") {
		return line // This is a file path line, do not process
	}

	// Remove package path (everything after the last slash).
	// If no space is found before the last slash (LastIndexByte returns -1),
	// the substring starts at index 0, which correctly handles lines without
	// a leading space (e.g., bare function names).
	if lastSlash := strings.LastIndexByte(line, '/'); lastSlash >= 0 {
		before := line[:strings.LastIndexByte(line[:lastSlash], ' ')+1]
		after := line[lastSlash+1:]
		line = before + after
	}

	// Fix center dot symbols (U+00B7) to normal dots
	line = strings.ReplaceAll(line, "·", ".")

	return line
}

// isBrokenPipe checks if the error is a broken pipe error.
// It uses errors.As to properly unwrap nested errors (e.g., fmt.Errorf("%w", opErr)),
// handling cases where the *net.OpError is wrapped inside another error type.
func isBrokenPipe(err any) bool {
	e, ok := err.(error)
	if !ok {
		return false
	}

	var ne *net.OpError
	if errors.As(e, &ne) {
		var se *os.SyscallError
		if errors.As(ne, &se) {
			seStr := strings.ToLower(se.Error())
			return strings.Contains(seStr, "broken pipe") ||
				strings.Contains(seStr, "connection reset by peer")
		}
	}
	return false
}
