package ginx

import "github.com/gin-gonic/gin"

// Chain 表示中间件链
type Chain struct {
	middlewares  []Middleware
	errorHandler ErrorHandler
}

// NewChain 创建一个新的中间件链
func NewChain() *Chain {
	return &Chain{
		middlewares: make([]Middleware, 0),
	}
}

// Use 添加一个中间件到链中
func (c *Chain) Use(m Middleware) *Chain {
	c.middlewares = append(c.middlewares, m)
	return c
}

// When 当条件为真时，添加中间件到链中
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

// Unless 当条件为假时，添加中间件到链中
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

// OnError 设置错误处理器
func (c *Chain) OnError(handler ErrorHandler) *Chain {
	c.errorHandler = handler
	return c
}

// Build 构建最终的 gin.HandlerFunc
func (c *Chain) Build() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// 创建执行链
		handler := func(ctx *gin.Context) {
			ctx.Next()
		}

		// 从后往前应用中间件
		for i := len(c.middlewares) - 1; i >= 0; i-- {
			handler = c.middlewares[i](handler)
		}

		// 执行中间件链
		handler(ctx)
	}
}
