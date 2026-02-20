package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
)

func main() {
	r := newRouter()

	log.Println("服务器启动在 :8080")
	log.Println("测试 URL:")
	log.Println("  http://localhost:8080/fast (预期超时)")
	log.Println("  http://localhost:8080/slow (预期超时)")
	log.Println("  http://localhost:8080/check-timeout (预期超时)")
	log.Println("  http://localhost:8080/api/heavy/process (预期超时)")

	r.Run(":8080")
}

func newRouter() *gin.Engine {
	r := gin.New()

	// 添加日志中间件来展示 IsTimeout helper 的用法
	r.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next() // 执行后续中间件和处理函数

		// 请求处理完成后记录日志，使用 IsTimeout helper 检查是否超时
		duration := time.Since(start)
		if ginx.IsTimeout(c) {
			log.Printf("请求超时 - 路径: %s, 耗时: %v", c.Request.URL.Path, duration)
		} else {
			log.Printf("请求完成 - 路径: %s, 耗时: %v, 状态码: %d", c.Request.URL.Path, duration, c.Writer.Status())
		}
	})

	// 使用链式配置：条件超时 - 不同路径不同超时
	chain := ginx.NewChain().
		// 统一错误响应格式
		WithErrorFormat(func(status int, msg string) any {
			return gin.H{"code": status, "message": msg}
		}).
		// 重型API使用长超时（60秒）
		When(ginx.PathHasPrefix("/api/heavy"), ginx.Timeout(
			ginx.WithTimeout(60*time.Second),
		)).
		// 慢接口使用中等超时（10秒）
		When(ginx.PathIs("/slow"), ginx.Timeout(
			ginx.WithTimeout(10*time.Second),
		)).
		// 其他接口使用短超时（5秒）
		Unless(ginx.Or(
			ginx.PathHasPrefix("/api/heavy"),
			ginx.PathIs("/slow"),
		), ginx.Timeout(ginx.WithTimeout(5*time.Second)))

	r.Use(chain.Build())

	// 测试路由
	r.GET("/fast", func(c *gin.Context) {
		time.Sleep(8 * time.Second)
		c.JSON(200, gin.H{"message": "快速响应"})
	})

	r.GET("/slow", func(c *gin.Context) {
		time.Sleep(15 * time.Second)
		c.JSON(200, gin.H{"message": "慢速响应"})
	})

	r.GET("/api/heavy/process", func(c *gin.Context) {
		time.Sleep(70 * time.Second)
		c.JSON(200, gin.H{"message": "重型处理完成"})
	})

	// 展示在业务逻辑中使用 IsTimeout helper
	r.GET("/check-timeout", func(c *gin.Context) {
		ctx := c.Request.Context()

		// 使用 Request.Context().Done() 检测当前处理流程是否已超时/取消
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			log.Println("请求已取消或超时，提前退出处理")
			return
		}

		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			log.Println("请求已取消或超时，提前退出处理")
			return
		}

		c.JSON(200, gin.H{
			"message": "处理完成",
		})
	})

	return r
}
