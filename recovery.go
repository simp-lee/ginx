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
	c.AbortWithStatusJSON(500, gin.H{
		"error":   "Internal Server Error",
		"message": "An unexpected error occurred",
	})
}

// Recovery creates a panic recovery middleware.
func Recovery(options ...logger.Option) Middleware {
	return RecoveryWith(nil, options...)
}

// RecoveryWith creates a panic recovery middleware with a custom handler.
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
	var buf [4096]byte
	n := runtime.Stack(buf[:], false)
	stack := string(buf[:n])

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
	// Only process function name lines (those without file paths)
	if strings.Contains(line, "/") && strings.Contains(line, ":") {
		return line // This is a file path line, do not process
	}

	// Remove package path (everything after the last slash)
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
func isBrokenPipe(err any) bool {
	if ne, ok := err.(*net.OpError); ok {
		var se *os.SyscallError
		if errors.As(ne, &se) {
			seStr := strings.ToLower(se.Error())
			return strings.Contains(seStr, "broken pipe") ||
				strings.Contains(seStr, "connection reset by peer")
		}
	}
	return false
}
