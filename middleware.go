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
