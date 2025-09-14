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

// Timeout 返回我们的 Middleware 类型，用于 Chain 和条件逻辑
func Timeout(options ...Option[TimeoutConfig]) Middleware {
	config := defaultTimeoutConfig()
	for _, option := range options {
		option(config)
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			// 创建超时上下文
			ctx, cancel := context.WithTimeout(c.Request.Context(), config.Timeout)
			defer cancel()

			// 设置新的上下文到请求中
			c.Request = c.Request.WithContext(ctx)

			// 用于通知处理完成的 channel
			finish := make(chan struct{}, 1)

			go func() {
				defer func() {
					if err := recover(); err != nil {
						// 通知处理完成（即使发生panic）
						select {
						case finish <- struct{}{}:
						default:
						}
						// 重新抛出 panic
						panic(err)
					}
					// 通知处理完成
					select {
					case finish <- struct{}{}:
					default:
					}
				}()

				// 执行下一个处理器
				next(c)
			}()

			// 等待处理完成或超时
			select {
			case <-finish:
				// 处理完成，正常返回
				return
			case <-ctx.Done():
				// 超时了，返回超时响应
				c.AbortWithStatusJSON(http.StatusRequestTimeout, config.Response)
				return
			}
		}
	}
}
