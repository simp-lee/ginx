package ginx

import "github.com/gin-gonic/gin"

// Chain is a middleware chain builder for Gin
type Chain struct {
	middlewares  []Middleware
	errorHandler ErrorHandler
}

// NewChain creates a new Chain instance
func NewChain() *Chain {
	return &Chain{
		middlewares: make([]Middleware, 0),
	}
}

// Use adds a middleware to the chain
func (c *Chain) Use(m Middleware) *Chain {
	c.middlewares = append(c.middlewares, m)
	return c
}

// When adds middleware to the chain if the condition is true
func (c *Chain) When(cond Condition, m Middleware) *Chain {
	conditionalMiddleware := func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(ctx *gin.Context) {
			if cond(ctx) {
				m(next)(ctx)
			} else {
				next(ctx)
			}
		}
	}
	c.middlewares = append(c.middlewares, conditionalMiddleware)
	return c
}

// Unless adds middleware to the chain if the condition is false
func (c *Chain) Unless(cond Condition, m Middleware) *Chain {
	conditionalMiddleware := func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(ctx *gin.Context) {
			if !cond(ctx) {
				m(next)(ctx)
			} else {
				next(ctx)
			}
		}
	}
	c.middlewares = append(c.middlewares, conditionalMiddleware)
	return c
}

// OnError sets the error handler for the chain
func (c *Chain) OnError(handler ErrorHandler) *Chain {
	c.errorHandler = handler
	return c
}

// Build builds the final gin.HandlerFunc
func (c *Chain) Build() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Create the execution chain
		handler := func(ctx *gin.Context) {
			ctx.Next()
		}

		// Apply middleware from last to first
		for i := len(c.middlewares) - 1; i >= 0; i-- {
			handler = c.middlewares[i](handler)
		}

		// Execute the middleware chain
		handler(ctx)

		// Check for errors after middleware chain execution
		if c.errorHandler != nil && len(ctx.Errors) > 0 {
			// Call error handler with the last error
			c.errorHandler(ctx, ctx.Errors.Last().Err)
		}
	}
}
