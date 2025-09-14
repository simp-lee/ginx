package main

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
)

func main() {
	r := gin.New()

	// 使用链式配置：条件超时 - 不同路径不同超时和响应
	chain := ginx.NewChain().
		// 重型API使用长超时（60秒）
		When(ginx.PathHasPrefix("/api/heavy"), ginx.Timeout(
			ginx.WithTimeout(60*time.Second),
			ginx.WithTimeoutMessage("重型任务处理超时，请稍后重试"),
		)).
		// 慢接口使用中等超时（10秒）
		When(ginx.PathIs("/slow"), ginx.Timeout(
			ginx.WithTimeout(10*time.Second),
			ginx.WithTimeoutResponse(gin.H{
				"error":   "服务器处理超时",
				"message": "请稍后重试",
				"code":    408,
			}),
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

	r.Run(":8080")
}
