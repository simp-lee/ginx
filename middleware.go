package ginx

import "github.com/gin-gonic/gin"

// Middleware 表示一个中间件函数，接受一个 HandlerFunc 并返回一个新的 HandlerFunc
type Middleware func(gin.HandlerFunc) gin.HandlerFunc

// Condition 表示一个条件函数，用于判断是否应该执行某个中间件
type Condition func(*gin.Context) bool

// Option 是一个泛型选项函数，用于配置各种结构
type Option[T any] func(*T)

// ErrorHandler 表示错误处理器类型
type ErrorHandler func(*gin.Context, error)
