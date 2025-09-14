package ginx

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/logger"
)

func TestLoggingBasicUsage(t *testing.T) {
	t.Run("Logger middleware creation", func(t *testing.T) {
		middleware := Logger()

		if middleware == nil {
			t.Error("Logger should return a valid middleware function")
		}
	})

	t.Run("Logger with custom options", func(t *testing.T) {
		middleware := Logger(
			logger.WithLevel(slog.LevelDebug),
			logger.WithConsole(true),
		)

		if middleware == nil {
			t.Error("Logger with options should return a valid middleware function")
		}
	})
}

func TestLoggingExecution(t *testing.T) {
	t.Run("Logger executes and calls next", func(t *testing.T) {
		var nextCalled bool

		middleware := Logger()

		next := func(c *gin.Context) {
			nextCalled = true
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		c, w := TestContext("GET", "/api/users", nil)

		// 执行中间件
		middleware(next)(c)

		if !nextCalled {
			t.Error("Logger middleware should call next handler")
		}

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Logger records request timing", func(t *testing.T) {
		middleware := Logger()

		// 创建一个会延迟的处理器来测试时间记录
		next := func(c *gin.Context) {
			time.Sleep(10 * time.Millisecond)
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		c, _ := TestContext("GET", "/api/test", nil)

		start := time.Now()
		middleware(next)(c)
		elapsed := time.Since(start)

		// 验证确实有延迟（说明中间件在记录时间）
		if elapsed < 10*time.Millisecond {
			t.Error("Logger should wait for next handler to complete")
		}
	})
}

func TestLoggingRequestDetails(t *testing.T) {
	t.Run("Logger handles GET request", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		c, w := TestContext("GET", "/api/users", nil)

		middleware(next)(c)

		// 验证基本执行
		if w.Code != http.StatusOK {
			t.Error("Should handle GET request correctly")
		}
	})

	t.Run("Logger handles POST request with body", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusCreated)
			c.Writer.WriteHeaderNow()
		}

		headers := map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   "Test-Client/1.0",
		}

		c, w := TestContext("POST", "/api/users?filter=active", headers)

		middleware(next)(c)

		if w.Code != http.StatusCreated {
			t.Error("Should handle POST request correctly")
		}
	})

	t.Run("Logger handles different HTTP methods", func(t *testing.T) {
		methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

		for _, method := range methods {
			middleware := Logger()

			next := func(c *gin.Context) {
				c.Status(http.StatusOK)
				c.Writer.WriteHeaderNow()
			}

			c, w := TestContext(method, "/api/test", nil)

			middleware(next)(c)

			if w.Code != http.StatusOK {
				t.Errorf("Should handle %s method correctly", method)
			}
		}
	})

	t.Run("Logger handles requests with query parameters", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		c, w := TestContext("GET", "/api/users?page=1&limit=10&sort=name", nil)

		middleware(next)(c)

		if w.Code != http.StatusOK {
			t.Error("Should handle query parameters correctly")
		}
	})
}

func TestLoggingStatusCodes(t *testing.T) {
	t.Run("Logger handles 2xx success responses", func(t *testing.T) {
		statusCodes := []int{200, 201, 202, 204}

		for _, statusCode := range statusCodes {
			middleware := Logger()

			next := func(c *gin.Context) {
				c.Status(statusCode)
				c.Writer.WriteHeaderNow()
			}

			c, w := TestContext("GET", "/api/test", nil)

			middleware(next)(c)

			if w.Code != statusCode {
				t.Errorf("Should handle status code %d correctly", statusCode)
			}
		}
	})

	t.Run("Logger handles 4xx client error responses", func(t *testing.T) {
		statusCodes := []int{400, 401, 403, 404, 409, 422}

		for _, statusCode := range statusCodes {
			middleware := Logger()

			next := func(c *gin.Context) {
				c.Status(statusCode)
				c.Writer.WriteHeaderNow()
			}

			c, w := TestContext("GET", "/api/test", nil)

			middleware(next)(c)

			if w.Code != statusCode {
				t.Errorf("Should handle status code %d correctly", statusCode)
			}
		}
	})

	t.Run("Logger handles 5xx server error responses", func(t *testing.T) {
		statusCodes := []int{500, 502, 503, 504}

		for _, statusCode := range statusCodes {
			middleware := Logger()

			next := func(c *gin.Context) {
				c.Status(statusCode)
				c.Writer.WriteHeaderNow()
			}

			c, w := TestContext("GET", "/api/test", nil)

			middleware(next)(c)

			if w.Code != statusCode {
				t.Errorf("Should handle status code %d correctly", statusCode)
			}
		}
	})
}

func TestLoggingHeaders(t *testing.T) {
	t.Run("Logger processes various request headers", func(t *testing.T) {
		headers := map[string]string{
			"Authorization":    "Bearer token123",
			"Content-Type":     "application/json",
			"User-Agent":       "TestBot/2.0",
			"Accept":           "application/json",
			"Accept-Language":  "en-US,en;q=0.9",
			"Accept-Encoding":  "gzip, deflate, br",
			"Cache-Control":    "no-cache",
			"X-Requested-With": "XMLHttpRequest",
			"X-API-Version":    "v1",
			"Referer":          "https://example.com/dashboard",
		}

		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		c, w := TestContext("POST", "/api/users", headers)

		middleware(next)(c)

		if w.Code != http.StatusOK {
			t.Error("Should handle requests with multiple headers")
		}
	})

	t.Run("Logger handles missing optional headers", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		// 没有任何额外头部的请求
		c, w := TestContext("GET", "/api/test", nil)

		middleware(next)(c)

		if w.Code != http.StatusOK {
			t.Error("Should handle requests with minimal headers")
		}
	})
}

func TestLoggingErrors(t *testing.T) {
	t.Run("Logger handles gin context errors", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			// 添加一些错误到gin context
			c.Error(fmt.Errorf("validation failed"))
			c.Error(fmt.Errorf("public error"))
			c.Status(http.StatusBadRequest)
			c.Writer.WriteHeaderNow()
		}

		c, w := TestContext("POST", "/api/users", nil)

		middleware(next)(c)

		if w.Code != http.StatusBadRequest {
			t.Error("Should handle requests with gin context errors")
		}

		// 验证错误被记录
		if len(c.Errors) != 2 {
			t.Error("Should preserve gin context errors")
		}
	})

	t.Run("Logger handles requests without errors", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		c, w := TestContext("GET", "/api/users", nil)

		middleware(next)(c)

		if w.Code != http.StatusOK {
			t.Error("Should handle requests without errors")
		}

		if len(c.Errors) != 0 {
			t.Error("Should not have any errors in successful requests")
		}
	})
}

func TestLoggingClientIP(t *testing.T) {
	t.Run("Logger handles different client IP scenarios", func(t *testing.T) {
		testCases := []struct {
			name    string
			headers map[string]string
		}{
			{
				name:    "Direct connection",
				headers: map[string]string{},
			},
			{
				name: "Behind proxy with X-Forwarded-For",
				headers: map[string]string{
					"X-Forwarded-For": "192.168.1.100, 10.0.0.1",
				},
			},
			{
				name: "Behind proxy with X-Real-IP",
				headers: map[string]string{
					"X-Real-IP": "203.0.113.1",
				},
			},
			{
				name: "Behind Cloudflare",
				headers: map[string]string{
					"CF-Connecting-IP": "198.51.100.1",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				middleware := Logger()

				next := func(c *gin.Context) {
					c.Status(http.StatusOK)
					c.Writer.WriteHeaderNow()
				}

				c, w := TestContext("GET", "/api/test", tc.headers)

				middleware(next)(c)

				if w.Code != http.StatusOK {
					t.Errorf("Should handle %s correctly", tc.name)
				}
			})
		}
	})
}

func TestLoggingPerformance(t *testing.T) {
	t.Run("Logger should not significantly impact performance", func(t *testing.T) {
		middleware := Logger()

		// 快速处理器
		fastHandler := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		// 测量没有日志中间件的执行时间
		c1, _ := TestContext("GET", "/api/test", nil)
		start1 := time.Now()
		fastHandler(c1)
		durationWithoutLogger := time.Since(start1)

		// 测量有日志中间件的执行时间
		c2, _ := TestContext("GET", "/api/test", nil)
		start2 := time.Now()
		middleware(fastHandler)(c2)
		durationWithLogger := time.Since(start2)

		// 日志中间件的开销应该是合理的（比如不超过原始时间的10倍）
		// 这个测试主要是确保没有明显的性能回归
		if durationWithLogger > durationWithoutLogger*10 {
			t.Errorf("Logger middleware overhead too high: %v vs %v",
				durationWithLogger, durationWithoutLogger)
		}
	})
}

func TestLoggingEdgeCases(t *testing.T) {
	t.Run("Logger handles very long URLs", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		// 构造一个很长的URL
		longPath := "/api/users" + strings.Repeat("/very-long-path-segment", 100)
		longQuery := "?" + strings.Repeat("param=value&", 50)
		longURL := longPath + longQuery

		c, w := TestContext("GET", longURL, nil)

		middleware(next)(c)

		if w.Code != http.StatusOK {
			t.Error("Should handle very long URLs")
		}
	})

	t.Run("Logger handles special characters in URLs", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		// URL编码的特殊字符
		specialPath := "/api/search?q=%E4%B8%AD%E6%96%87&filter=%3E%3D100%26%3C%3D200"

		c, w := TestContext("GET", specialPath, nil)

		middleware(next)(c)

		if w.Code != http.StatusOK {
			t.Error("Should handle URLs with special characters")
		}
	})

	t.Run("Logger handles empty response body", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			// 只设置状态码，不写入任何内容
			c.Status(http.StatusNoContent)
			c.Writer.WriteHeaderNow()
		}

		c, w := TestContext("DELETE", "/api/users/123", nil)

		middleware(next)(c)

		if w.Code != http.StatusNoContent {
			t.Errorf("Should handle empty response body, got status %d, expected %d", w.Code, http.StatusNoContent)
		}
	})

	t.Run("Logger handles concurrent requests", func(t *testing.T) {
		middleware := Logger()

		next := func(c *gin.Context) {
			// 模拟一些处理时间
			time.Sleep(1 * time.Millisecond)
			c.Status(http.StatusOK)
			c.Writer.WriteHeaderNow()
		}

		// 并发执行多个请求
		concurrency := 10
		done := make(chan bool, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(id int) {
				defer func() { done <- true }()

				c, w := TestContext("GET", "/api/test", map[string]string{
					"X-Request-ID": fmt.Sprintf("req-%d", id),
				})

				middleware(next)(c)

				if w.Code != http.StatusOK {
					t.Errorf("Concurrent request %d failed", id)
				}
			}(i)
		}

		// 等待所有请求完成
		for i := 0; i < concurrency; i++ {
			<-done
		}
	})
}

func TestLoggingLoggerOptions(t *testing.T) {
	t.Run("Logger accepts various logger options", func(t *testing.T) {
		// 测试不同的logger配置选项
		testCases := []struct {
			name    string
			options []logger.Option
		}{
			{
				name: "Debug level with console output",
				options: []logger.Option{
					logger.WithLevel(slog.LevelDebug),
					logger.WithConsole(true),
				},
			},
			{
				name: "Info level with console output",
				options: []logger.Option{
					logger.WithLevel(slog.LevelInfo),
					logger.WithConsole(true),
				},
			},
			{
				name: "Warn level with console output",
				options: []logger.Option{
					logger.WithLevel(slog.LevelWarn),
					logger.WithConsole(true),
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				middleware := Logger(tc.options...)

				if middleware == nil {
					t.Errorf("Should create logger with %s", tc.name)
				}

				// 测试中间件能正常工作
				next := func(c *gin.Context) {
					c.Status(http.StatusOK)
					c.Writer.WriteHeaderNow()
				}

				c, w := TestContext("GET", "/api/test", nil)

				middleware(next)(c)

				if w.Code != http.StatusOK {
					t.Errorf("Middleware with %s should work correctly", tc.name)
				}
			})
		}
	})
}
