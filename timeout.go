package ginx

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type TimeoutConfig struct {
	Timeout  time.Duration
	Response gin.H
}

func defaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		Timeout: 30 * time.Second,
		Response: gin.H{
			"error": "request timeout",
			"code":  408,
		},
	}
}

func WithTimeout(duration time.Duration) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.Timeout = duration
	}
}

func WithTimeoutResponse(response gin.H) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.Response = response
	}
}

func WithTimeoutMessage(message string) Option[TimeoutConfig] {
	return func(c *TimeoutConfig) {
		c.Response = gin.H{
			"error": message,
			"code":  408,
		}
	}
}

// Timeout middleware to set a timeout for requests.
func Timeout(options ...Option[TimeoutConfig]) Middleware {
	config := defaultTimeoutConfig()
	for _, option := range options {
		option(config)
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
			defer cancel()

			// Set the new context to the request
			c.Request = c.Request.WithContext(ctx)

			// Channel to notify when processing is complete
			finish := make(chan struct{}, 1)

			go func() {
				defer func() {
					if err := recover(); err != nil {
						// Notify that processing is complete (even if a panic occurred)
						select {
						case finish <- struct{}{}:
						default:
						}
						// Re-throw panic
						panic(err)
					}
					// Notify that processing is complete
					select {
					case finish <- struct{}{}:
					default:
					}
				}()

				// Execute the next handler
				next(c)
			}()

			// Wait for processing to complete or timeout
			select {
			case <-finish:
				// Processing complete, return normally
				return
			case <-ctx.Done():
				// Timeout occurred, return timeout response
				c.AbortWithStatusJSON(http.StatusRequestTimeout, config.Response)
				return
			}
		}
	}
}
