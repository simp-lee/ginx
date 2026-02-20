package ginx

import "github.com/gin-gonic/gin"

// Middleware represents a middleware function that takes a HandlerFunc and returns a new HandlerFunc.
type Middleware func(gin.HandlerFunc) gin.HandlerFunc

// Condition represents a condition function that determines whether a middleware should be executed.
type Condition func(*gin.Context) bool

// Option represents a generic option function for configuring various structures.
type Option[T any] func(*T)

// ErrorHandler represents an error handler function type.
type ErrorHandler func(*gin.Context, error)

// ErrorFormatter transforms middleware error responses into a custom format.
// It receives the HTTP status code and default error message,
// and returns the response body to be serialized as JSON.
type ErrorFormatter func(status int, message string) any

// ErrorFormat creates a middleware that sets the ErrorFormatter for downstream middleware.
// Use this when not using Chain, or when you need per-route-group formatting.
func ErrorFormat(f ErrorFormatter) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			SetErrorFormatter(c, f)
			next(c)
		}
	}
}
