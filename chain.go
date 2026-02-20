package ginx

import "github.com/gin-gonic/gin"

// Chain is a middleware chain builder for Gin
type Chain struct {
	middlewares    []Middleware
	errorHandler   ErrorHandler
	errorFormatter ErrorFormatter
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

// WithErrorFormat sets the ErrorFormatter for the chain.
// When set, the formatter is injected into every request context
// before middleware execution, making it available via GetErrorFormatter.
func (c *Chain) WithErrorFormat(f ErrorFormatter) *Chain {
	c.errorFormatter = f
	return c
}

// Build builds the final gin.HandlerFunc.
// The middleware chain is constructed once at setup time, not per request.
func (c *Chain) Build() gin.HandlerFunc {
	// Build chain once at setup time — avoids O(N) heap allocations per request.
	handler := func(ctx *gin.Context) {
		ctx.Next()
	}
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		handler = c.middlewares[i](handler)
	}
	f := c.errorFormatter
	eh := c.errorHandler
	return func(ctx *gin.Context) {
		// Inject error formatter into context if set
		if f != nil {
			SetErrorFormatter(ctx, f)
		}
		handler(ctx)
		// Check for errors after middleware chain execution
		if eh != nil && len(ctx.Errors) > 0 {
			eh(ctx, ctx.Errors.Last().Err)
		}
	}
}
