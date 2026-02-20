package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
	"github.com/simp-lee/logger"
)

func main() {
	r := newRouter()

	fmt.Println("🚀 服务器启动在 :8080")
	fmt.Println("\n📋 测试路由：")
	fmt.Println("  GET /fast       - 快速响应 (INFO 日志)")
	fmt.Println("  GET /slow       - 超时响应 (INFO + WARN 双重日志)")
	fmt.Println("  GET /maybe-slow - 接近超时边界")
	fmt.Println("\n💡 架构亮点：")
	fmt.Println("  - Logger 中间件负责所有请求的基础日志")
	fmt.Println("  - Timeout 中间件专注于超时处理")
	fmt.Println("  - 通过 c.Next() 后 IsTimeout() 识别超时请求")
	fmt.Println("  - 独立后置中间件实现超时日志增强")
	fmt.Println("  - 职责分离，组合优于继承 🎯")

	log.Fatal(http.ListenAndServe(":8080", r))
}

func newRouter() *gin.Engine {
	gin.SetMode(gin.DebugMode)

	r := gin.New()

	// 🎯 精巧的组合式设计：使用 Chain 和 Condition 来组合中间件
	// 这展示了架构设计的优雅之处：每个中间件职责单一，通过组合实现复杂功能

	chain := ginx.NewChain().
		// 统一错误响应格式
		WithErrorFormat(func(status int, msg string) any {
			return gin.H{"code": status, "message": msg}
		}).
		// 1. 普通请求的日志记录（INFO 级别）
		Use(ginx.Logger(
			logger.WithConsole(true),
		)).
		// 2. 超时中间件
		Use(ginx.Timeout(
			ginx.WithTimeout(2 * time.Second),
		))

	r.Use(chain.Build())

	// 3. 🔥 关键亮点：在 c.Next() 后检查 IsTimeout，仅对超时请求输出额外日志
	r.Use(func(c *gin.Context) {
		c.Next()
		if ginx.IsTimeout(c) {
			log.Printf("[WARN] timeout request method=%s path=%s status=%d", c.Request.Method, c.Request.URL.Path, c.Writer.Status())
		}
	})

	// 正常响应的路由
	r.GET("/fast", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "快速响应"})
	})

	// 会超时的路由
	r.GET("/slow", func(c *gin.Context) {
		// 模拟慢请求，会触发超时
		time.Sleep(3 * time.Second)
		c.JSON(200, gin.H{"message": "慢响应"})
	})

	// 部分超时的路由（有时超时，有时不超时）
	r.GET("/maybe-slow", func(c *gin.Context) {
		// 模拟1.5秒的处理时间，接近但不超过超时限制
		time.Sleep(1500 * time.Millisecond)
		c.JSON(200, gin.H{"message": "可能慢的响应"})
	})

	return r
}
