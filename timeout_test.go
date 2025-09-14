package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTimeoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("request completes within timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(100 * time.Millisecond)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			time.Sleep(50 * time.Millisecond)
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "success")
	})

	t.Run("request times out", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// 使用带有超时检查的 sleep
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				// 如果上下文被取消（超时），直接返回
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "request timeout")
	})

	t.Run("custom timeout response", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(
			WithTimeout(50*time.Millisecond),
			WithTimeoutResponse(gin.H{"error": "custom timeout", "status": "timeout"}),
		))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// 使用带有超时检查的 sleep
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				// 如果上下文被取消（超时），直接返回
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "custom timeout")
	})

	t.Run("custom timeout message", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(
			WithTimeout(50*time.Millisecond),
			WithTimeoutMessage("处理超时"),
		))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// 使用带有超时检查的 sleep
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				// 如果上下文被取消（超时），直接返回
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "处理超时")
	})

	t.Run("default timeout configuration", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout())
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "success")
	})
}

func TestTimeoutInChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("conditional timeout using Chain", func(t *testing.T) {
		r := gin.New()

		// 使用 Chain 的条件逻辑来应用不同的超时
		chain := NewChain().
			When(PathHasPrefix("/api/heavy"), Timeout(WithTimeout(200*time.Millisecond))).
			When(PathHasPrefix("/api/"), Timeout(WithTimeout(100*time.Millisecond))).
			Unless(PathHasPrefix("/api/"), Timeout(WithTimeout(50*time.Millisecond)))

		r.Use(chain.Build())

		r.GET("/api/test", func(c *gin.Context) {
			time.Sleep(75 * time.Millisecond)
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "success")
	})

	t.Run("multiple conditional timeouts", func(t *testing.T) {
		r := gin.New()

		// 为不同路径设置不同的超时
		chain := NewChain().
			When(PathHasPrefix("/api/heavy"), Timeout(WithTimeout(200*time.Millisecond))).
			Unless(PathHasPrefix("/api/heavy"), Timeout(WithTimeout(50*time.Millisecond)))

		r.Use(chain.Build())

		r.GET("/api/heavy/process", func(c *gin.Context) {
			// 使用带有超时检查的 sleep
			select {
			case <-time.After(100 * time.Millisecond):
			case <-c.Request.Context().Done():
				return
			}
			c.JSON(200, gin.H{"message": "heavy task completed"})
		})

		r.GET("/api/normal", func(c *gin.Context) {
			// 使用带有超时检查的 sleep
			select {
			case <-time.After(100 * time.Millisecond):
			case <-c.Request.Context().Done():
				return
			}
			c.JSON(200, gin.H{"message": "normal task"})
		})

		// Heavy API should succeed (100ms < 200ms timeout)
		w1 := httptest.NewRecorder()
		req1, _ := http.NewRequest("GET", "/api/heavy/process", nil)
		r.ServeHTTP(w1, req1)
		assert.Equal(t, 200, w1.Code)

		// Normal API should timeout (100ms > 50ms timeout)
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("GET", "/api/normal", nil)
		r.ServeHTTP(w2, req2)
		assert.Equal(t, 408, w2.Code)
	})
}

func TestTimeoutConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("concurrent requests with timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(75 * time.Millisecond)))
		r.Use(chain.Build())

		r.GET("/fast", func(c *gin.Context) {
			// 快速完成
			time.Sleep(50 * time.Millisecond)
			c.JSON(200, gin.H{"message": "success"})
		})

		r.GET("/slow", func(c *gin.Context) {
			// 超时
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return
			}
		})

		const numRequests = 20
		results := make(chan int, numRequests)

		// 一半请求发送到快速端点，一半发送到慢速端点
		for i := 0; i < numRequests; i++ {
			endpoint := "/fast"
			if i%2 == 0 {
				endpoint = "/slow"
			}
			go func(ep string) {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", ep, nil)
				r.ServeHTTP(w, req)
				results <- w.Code
			}(endpoint)
		}

		// 收集结果
		successCount := 0
		timeoutCount := 0
		for i := 0; i < numRequests; i++ {
			code := <-results
			switch code {
			case 200:
				successCount++
			case 408:
				timeoutCount++
			}
		}

		// 应该既有成功的请求，也有超时的请求
		assert.True(t, successCount > 0, "Should have some successful requests")
		assert.True(t, timeoutCount > 0, "Should have some timeout requests")
		assert.Equal(t, numRequests, successCount+timeoutCount, "All requests should be accounted for")
	})

	t.Run("race condition test", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())

		processedCount := 0
		r.GET("/test", func(c *gin.Context) {
			// 模拟竞态条件：检查上下文是否取消
			select {
			case <-c.Request.Context().Done():
				return
			default:
			}

			// 增加处理计数
			processedCount++

			// 再次检查上下文
			select {
			case <-time.After(75 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success", "count": processedCount})
			case <-c.Request.Context().Done():
				return
			}
		})

		const numRequests = 20
		results := make(chan int, numRequests)

		// 快速并发发送请求
		for i := 0; i < numRequests; i++ {
			go func() {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/test", nil)
				r.ServeHTTP(w, req)
				results <- w.Code
			}()
		}

		// 收集结果
		timeoutCount := 0
		for i := 0; i < numRequests; i++ {
			code := <-results
			if code == 408 {
				timeoutCount++
			}
		}

		// 大部分请求应该超时
		assert.True(t, timeoutCount > numRequests/2, "Most requests should timeout")
	})
}

func TestTimeoutPanicHandling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("panic in handler before timeout", func(t *testing.T) {
		r := gin.New()

		chain := NewChain().
			Use(Timeout(WithTimeout(100 * time.Millisecond))).
			Use(Recovery())
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			panic("test panic")
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)

		// 不应该panic，应该被recovery中间件处理
		assert.NotPanics(t, func() {
			r.ServeHTTP(w, req)
		})

		// 应该返回500错误（我们的Recovery的默认行为）
		assert.Equal(t, 500, w.Code)
		assert.Contains(t, w.Body.String(), "Internal Server Error")
	})

	t.Run("panic in handler after timeout", func(t *testing.T) {
		r := gin.New()

		chain := NewChain().
			Use(Timeout(WithTimeout(50 * time.Millisecond))).
			Use(Recovery())
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// 等待超过超时时间
			time.Sleep(100 * time.Millisecond)
			panic("test panic after timeout")
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)

		assert.NotPanics(t, func() {
			r.ServeHTTP(w, req)
		})

		// 应该返回超时错误，不是panic错误
		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "request timeout")
	})
}

func TestTimeoutEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("zero timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(0)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// 零超时应该立即超时
		assert.Equal(t, 408, w.Code)
	})

	t.Run("negative timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(-1 * time.Second)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// 负超时应该立即超时
		assert.Equal(t, 408, w.Code)
	})

	t.Run("very long timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(10 * time.Second)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// 快速完成
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// 应该在超时前完成
		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "success")
	})

	t.Run("exactly at timeout boundary", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// 睡眠时间接近超时时间
			select {
			case <-time.After(45 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// 应该在超时前完成
		assert.Equal(t, 200, w.Code)
	})
}
