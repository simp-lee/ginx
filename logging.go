package ginx

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/logger"
)

// Logger create a logging middleware with the given options.
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
				"query", c.Request.URL.RawQuery,
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
