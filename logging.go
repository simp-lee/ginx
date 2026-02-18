package ginx

import (
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/logger"
)

// Logger creates a logging middleware with the given options.
//
// NOTE: This function panics if the logger cannot be created (e.g., invalid file path).
// This follows the same pattern as regexp.MustCompile — configuration errors are caught
// at initialization time rather than at request time. Callers should ensure valid logger
// options are provided.
func Logger(options ...logger.Option) Middleware {
	// Initialize the logger
	log, err := logger.New(options...)
	if err != nil {
		panic("failed to create logger: " + err.Error())
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			// Start timer
			start := time.Now()

			// Process request
			next(c)

			// Log request details
			path := c.Request.URL.Path
			latency := time.Since(start)
			status := c.Writer.Status()

			// Prepare log fields
			fields := []any{
				"method", c.Request.Method,
				"path", path,
				"query", sanitizeQueryForLog(c.Request.URL.RawQuery),
				"status", status,
				"latency", latency,
				"ip", c.ClientIP(),
				"user_agent", c.Request.UserAgent(),
				"size", c.Writer.Size(),
				"protocol", c.Request.Proto,
				"referer", c.Request.Referer(),
			}

			if rid, ok := GetRequestID(c); ok && rid != "" {
				fields = append(fields, "request_id", rid)
			}

			// Log based on status code
			switch {
			case status >= 500:
				log.Error("HTTP Request", fields...)
			case status >= 400:
				log.Warn("HTTP Request", fields...)
			default:
				log.Info("HTTP Request", fields...)
			}

			// Log errors if any
			if len(c.Errors) > 0 {
				errFields := []any{"path", path, "errors", c.Errors.String()}
				if rid, ok := GetRequestID(c); ok && rid != "" {
					errFields = append(errFields, "request_id", rid)
				}
				log.Error("Request errors", errFields...)
			}
		}
	}
}

func sanitizeQueryForLog(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[UNPARSEABLE_QUERY]"
	}

	for key := range values {
		if isSensitiveQueryKey(key) {
			values.Set(key, "[REDACTED]")
		}
	}

	return values.Encode()
}

func isSensitiveQueryKey(key string) bool {
	lower := strings.ToLower(key)
	switch lower {
	case "token", "access_token", "id_token", "jwt", "authorization", "auth", "password", "secret":
		return true
	default:
		return false
	}
}
