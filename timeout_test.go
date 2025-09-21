package ginx

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
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
			// Use sleep with timeout checking
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				// Return directly if context is canceled (timeout)
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
		customResponse := gin.H{
			"error": "Request took too long",
			"code":  "TIMEOUT",
		}
		chain := NewChain().Use(Timeout(
			WithTimeout(50*time.Millisecond),
			WithTimeoutResponse(customResponse),
		))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "Request took too long")
		assert.Contains(t, w.Body.String(), "TIMEOUT")
	})

	t.Run("custom timeout message", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(
			WithTimeout(50*time.Millisecond),
			WithTimeoutMessage("Service temporarily unavailable"),
		))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "Service temporarily unavailable")
	})

	t.Run("default timeout configuration", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout()) // Use default configuration
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

	t.Run("timeout response size accuracy", func(t *testing.T) {
		r := gin.New()

		var recordedSize int
		var recordedStatus int
		var isTimeoutFlag bool

		// Add logging middleware to capture size and status
		r.Use(func(c *gin.Context) {
			c.Next()
			// Record final state after all middleware processing
			recordedSize = c.Writer.Size()
			recordedStatus = c.Writer.Status()
			isTimeoutFlag = IsTimeout(c)
		})

		chain := NewChain().Use(Timeout(
			WithTimeout(50*time.Millisecond),
			WithTimeoutResponse(gin.H{
				"code":    408,
				"message": "timeout",
			}),
		))
		r.Use(chain.Build())

		r.GET("/test", func(c *gin.Context) {
			// Write large business data before timeout
			largeData := make([]byte, 1024) // 1KB data
			for i := range largeData {
				largeData[i] = 'A'
			}

			// Write business response data to buffer
			c.Writer.Write(largeData)

			// Sleep to trigger timeout
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Verify timeout occurred
		assert.Equal(t, 408, w.Code)
		assert.True(t, isTimeoutFlag)
		assert.Equal(t, 408, recordedStatus)

		// Verify Size() returns actual timeout response size, not buffered business data size
		actualTimeoutResponseSize := len(w.Body.Bytes())
		assert.Equal(t, actualTimeoutResponseSize, recordedSize)

		// Verify it's not the large business data size (1024 bytes)
		assert.NotEqual(t, 1024, recordedSize)

		// Verify actual response content
		assert.Contains(t, w.Body.String(), "timeout")
		assert.Contains(t, w.Body.String(), "408")
	})
}

func TestTimeoutInChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("timeout with other middlewares", func(t *testing.T) {
		r := gin.New()

		// Use chained middleware
		chain := NewChain().
			Use(Logger()).
			Use(Timeout(WithTimeout(100 * time.Millisecond))).
			Use(Recovery())

		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// Fast request
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "success")
	})

	t.Run("timeout with slow handler", func(t *testing.T) {
		r := gin.New()

		// Timeout middleware in the chain
		chain := NewChain().
			Use(Timeout(WithTimeout(50 * time.Millisecond)))

		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "request timeout")
	})
}

func TestTimeoutConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("concurrent requests with timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(100 * time.Millisecond)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// Decide whether to timeout based on request parameters
			if c.Query("delay") == "long" {
				select {
				case <-time.After(150 * time.Millisecond):
					c.JSON(200, gin.H{"message": "long success"})
				case <-c.Request.Context().Done():
					return
				}
			} else {
				time.Sleep(50 * time.Millisecond)
				c.JSON(200, gin.H{"message": "short success"})
			}
		})

		var wg sync.WaitGroup
		results := make(chan struct {
			code int
			body string
		}, 10)

		// Send 5 fast requests and 5 slow requests concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				w := httptest.NewRecorder()
				var req *http.Request
				if i%2 == 0 {
					req, _ = http.NewRequest("GET", "/test", nil)
				} else {
					req, _ = http.NewRequest("GET", "/test?delay=long", nil)
				}

				r.ServeHTTP(w, req)
				results <- struct {
					code int
					body string
				}{w.Code, w.Body.String()}
			}(i)
		}

		wg.Wait()
		close(results)

		successCount := 0
		timeoutCount := 0

		for result := range results {
			switch result.code {
			case 200:
				successCount++
				assert.Contains(t, result.body, "short success")
			case 408:
				timeoutCount++
				assert.Contains(t, result.body, "request timeout")
			}
		}

		assert.Equal(t, 5, successCount, "Should have 5 successful requests")
		assert.Equal(t, 5, timeoutCount, "Should have 5 timeout requests")
	})

	t.Run("race condition test", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())

		// Simulate possible race conditions
		r.GET("/test", func(c *gin.Context) {
			// This handler ignores context - will always try to take 75ms
			time.Sleep(75 * time.Millisecond)
			c.JSON(200, gin.H{"message": "success"})
		})

		// Run multiple times to detect potential race conditions
		for i := 0; i < 20; i++ {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)

			// Should not panic
			assert.NotPanics(t, func() {
				r.ServeHTTP(w, req)
			})

			// Should be a timeout response because handler takes 75ms > 50ms timeout
			assert.Equal(t, 408, w.Code)
		}
	})

	t.Run("buffered writer concurrent access", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(200 * time.Millisecond)))
		r.Use(chain.Build())

		// Test the safety of multiple goroutines writing to the same response concurrently
		r.GET("/test", func(c *gin.Context) {
			var wg sync.WaitGroup
			var mu sync.Mutex // Protect concurrent access to gin.Context

			// Start multiple goroutines for concurrent writing
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					// Use lock to protect header setting, but directly test Writer's concurrent safety
					mu.Lock()
					c.Header(fmt.Sprintf("X-Goroutine-%d", id), "test")
					mu.Unlock()
					// Test bufferedWriter's concurrent WriteString
					fmt.Fprintf(c.Writer, "data-%d ", id)
				}(i)
			}

			wg.Wait()
			c.JSON(200, gin.H{"message": "concurrent writes completed"})
		})

		// Run multiple times to detect race conditions
		for i := 0; i < 50; i++ {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)

			assert.NotPanics(t, func() {
				r.ServeHTTP(w, req)
			}, "Concurrent writes should not panic")

			assert.Equal(t, 200, w.Code, "Request should complete successfully")
		}
	})

	t.Run("timeout vs normal completion race", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(100 * time.Millisecond)))
		r.Use(chain.Build())

		// Test race conditions between timeout and normal completion
		r.GET("/test", func(c *gin.Context) {
			// Random delay, sometimes timeout, sometimes not
			delay := time.Duration(50+rand.Intn(100)) * time.Millisecond

			select {
			case <-time.After(delay):
				// May complete near timeout boundary
				c.JSON(200, gin.H{"message": "success", "delay": delay.String()})
			case <-c.Request.Context().Done():
				return
			}
		})

		var successCount, timeoutCount int
		var mu sync.Mutex

		// Run multiple requests concurrently
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/test", nil)

				assert.NotPanics(t, func() {
					r.ServeHTTP(w, req)
				})

				mu.Lock()
				switch w.Code {
				case 200:
					successCount++
				case 408:
					timeoutCount++
				}
				mu.Unlock()
			}()
		}

		wg.Wait()

		// Verify result reasonableness
		total := successCount + timeoutCount
		assert.Equal(t, 100, total, "All requests should complete")
		assert.Greater(t, successCount, 0, "Some requests should succeed")
		assert.Greater(t, timeoutCount, 0, "Some requests should timeout")
		t.Logf("Success: %d, Timeout: %d", successCount, timeoutCount)
	})

	t.Run("hijack and flush concurrency", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(100 * time.Millisecond)))
		r.Use(chain.Build())

		r.GET("/test", func(c *gin.Context) {
			var wg sync.WaitGroup

			// Concurrently call various write methods
			wg.Add(3)

			go func() {
				defer wg.Done()
				c.Writer.Flush()
			}()

			go func() {
				defer wg.Done()
				c.Writer.WriteString("test data")
			}()

			go func() {
				defer wg.Done()
				// In test environment, try calling Hijack method, expecting error instead of panic
				_, _, err := c.Writer.Hijack()
				// In test environment, should return http.ErrNotSupported, which is normal
				assert.Equal(t, http.ErrNotSupported, err, "Hijack should return ErrNotSupported in test env")
			}()

			wg.Wait()
			c.JSON(200, gin.H{"message": "success"})
		})

		// Run multiple times to detect concurrent safety
		for i := 0; i < 20; i++ {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)

			assert.NotPanics(t, func() {
				r.ServeHTTP(w, req)
			}, "Concurrent Hijack/Flush should not panic")
		}
	})

	t.Run("IsTimeout function concurrency", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())

		var timeoutResults []bool
		var mu sync.Mutex

		r.GET("/test", func(c *gin.Context) {
			var wg sync.WaitGroup

			// Multiple goroutines concurrently check IsTimeout status
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					result := IsTimeout(c)
					mu.Lock()
					timeoutResults = append(timeoutResults, result)
					mu.Unlock()
				}()
			}

			select {
			case <-time.After(100 * time.Millisecond):
				wg.Wait()
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				wg.Wait()
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)

		assert.NotPanics(t, func() {
			r.ServeHTTP(w, req)
		})

		// Verify that all IsTimeout calls return the same result
		if len(timeoutResults) > 0 {
			firstResult := timeoutResults[0]
			for _, result := range timeoutResults {
				assert.Equal(t, firstResult, result, "All IsTimeout calls should return same result")
			}
		}
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

		// Zero timeout should immediately timeout
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

		// Negative timeout should immediately timeout
		assert.Equal(t, 408, w.Code)
	})

	t.Run("very long timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(10 * time.Second)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// Complete quickly
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Should complete before timeout
		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "success")
	})

	t.Run("exactly at timeout boundary", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			// Sleep time close to timeout duration
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

		// Should complete before timeout
		assert.Equal(t, 200, w.Code)
	})
}

func TestIsTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("IsTimeout returns true for timed out requests", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			select {
			case <-time.After(100 * time.Millisecond):
				// This should not be executed because it will timeout
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				// Return directly when context is canceled
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Request should timeout
		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "request timeout")

		// Verify timeout header marker
		assert.Equal(t, "true", w.Header().Get("X-Timeout"))
	})

	t.Run("IsTimeout returns false for normal requests", func(t *testing.T) {
		var isTimeoutResult bool

		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(100 * time.Millisecond)))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			time.Sleep(20 * time.Millisecond) // Complete quickly
			isTimeoutResult = IsTimeout(c)
			c.JSON(200, gin.H{"message": "success", "is_timeout": isTimeoutResult})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Request should succeed
		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "success")
		assert.False(t, isTimeoutResult, "IsTimeout should return false for normal requests")
	})

	t.Run("IsTimeout with custom timeout response", func(t *testing.T) {
		r := gin.New()
		customResponse := gin.H{
			"error": "Request timeout occurred",
			"code":  "TIMEOUT_ERROR",
		}
		chain := NewChain().Use(Timeout(
			WithTimeout(50*time.Millisecond),
			WithTimeoutResponse(customResponse),
		))
		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			select {
			case <-time.After(100 * time.Millisecond):
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Verify timeout response
		assert.Equal(t, 408, w.Code)
		assert.Contains(t, w.Body.String(), "Request timeout occurred")
		assert.Contains(t, w.Body.String(), "TIMEOUT_ERROR")

		// Verify timeout header
		assert.Equal(t, "true", w.Header().Get("X-Timeout"))
	})
}

// TestTimeoutChain tests that the timeout middleware correctly respects the chain
func TestTimeoutChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("timeout middleware respects chain order", func(t *testing.T) {
		var executionOrder []string

		// Create middleware that records execution order
		logMiddleware := func(name string) Middleware {
			return func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					executionOrder = append(executionOrder, name+"_before")
					next(c)
					executionOrder = append(executionOrder, name+"_after")
				}
			}
		}

		r := gin.New()
		chain := NewChain().
			Use(logMiddleware("First")).
			Use(Timeout(WithTimeout(100 * time.Millisecond))). // Timeout now respects the chain
			Use(logMiddleware("Third"))                        // This SHOULD now be executed

		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			executionOrder = append(executionOrder, "handler")
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Now Third middleware SHOULD execute because chain is fixed
		assert.Contains(t, executionOrder, "First_before", "First middleware should execute")
		assert.Contains(t, executionOrder, "First_after", "First middleware should complete")
		assert.Contains(t, executionOrder, "Third_before", "Third middleware should now execute (chain fixed)")
		assert.Contains(t, executionOrder, "Third_after", "Third middleware should now execute (chain fixed)")
		assert.Contains(t, executionOrder, "handler", "Handler should execute")

		expected := []string{
			"First_before", "Third_before", "handler", "Third_after", "First_after",
		}
		assert.Equal(t, expected, executionOrder, "Chain order should be correct after fix")
		assert.Equal(t, 200, w.Code)
	})

	t.Run("recovery works with timeout in chain", func(t *testing.T) {
		var panicCaught bool

		// Custom recovery that sets a flag
		customRecovery := func(next gin.HandlerFunc) gin.HandlerFunc {
			return func(c *gin.Context) {
				defer func() {
					if err := recover(); err != nil {
						panicCaught = true
						c.AbortWithStatusJSON(500, gin.H{"error": "Custom recovery caught panic"})
					}
				}()
				next(c)
			}
		}

		r := gin.New()

		// Test: Recovery after Timeout (Recovery should now work - this is the fix)
		chain := NewChain().
			Use(Timeout(WithTimeout(100 * time.Millisecond))).
			Use(Middleware(customRecovery)) // This should now work!

		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			panic("test panic")
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)

		panicCaught = false
		// This should NOT panic because Recovery now works correctly
		assert.NotPanics(t, func() {
			r.ServeHTTP(w, req)
		})
		assert.True(t, panicCaught, "Recovery should catch panic even when after Timeout")
		assert.Equal(t, 500, w.Code)
	})

	t.Run("timeout with multiple middlewares in chain", func(t *testing.T) {
		var middlewareExecuted = make(map[string]bool)

		trackingMiddleware := func(name string) Middleware {
			return func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					middlewareExecuted[name] = true
					next(c)
				}
			}
		}

		r := gin.New()
		chain := NewChain().
			Use(trackingMiddleware("Middleware-1")).
			Use(trackingMiddleware("Middleware-2")).
			Use(Timeout(WithTimeout(1 * time.Second))). // This should not break the chain
			Use(trackingMiddleware("Middleware-3")).    // This should now execute
			Use(trackingMiddleware("Middleware-4"))     // This should now execute

		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "success"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Verify ALL middlewares were executed (chain is NOT broken)
		assert.True(t, middlewareExecuted["Middleware-1"], "Middleware-1 should execute")
		assert.True(t, middlewareExecuted["Middleware-2"], "Middleware-2 should execute")
		assert.True(t, middlewareExecuted["Middleware-3"], "Middleware-3 should execute (chain fixed)")
		assert.True(t, middlewareExecuted["Middleware-4"], "Middleware-4 should execute (chain fixed)")

		assert.Equal(t, 200, w.Code)
		t.Logf("All middlewares executed correctly: %+v", middlewareExecuted)
	})
}

// TestTimeoutPreemption tests the behavior of our serial timeout implementation
// These tests document the current limitations and expected behavior
func TestTimeoutPreemption(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("handler ignores context cancellation - timeout detected post-execution", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())

		r.GET("/ignore-context", func(c *gin.Context) {
			// Handler that ignores context cancellation
			// This simulates poorly written handlers that don't cooperate with timeout
			ctx := c.Request.Context()

			for i := 0; i < 10; i++ {
				if ctx.Err() != nil {
					t.Logf("Context cancelled but handler ignores it: %v", ctx.Err())
				}

				// Handler continues working despite context cancellation
				time.Sleep(20 * time.Millisecond) // Total: 200ms, much > 50ms timeout
			}

			c.JSON(200, gin.H{"message": "completed despite timeout"})
		})

		start := time.Now()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/ignore-context", nil)
		r.ServeHTTP(w, req)
		elapsed := time.Since(start)

		t.Logf("Request completed in %v", elapsed)
		t.Logf("Response code: %d", w.Code)

		// With serial implementation: timeout is detected after handler completes
		// This is expected behavior - we can't preempt, but we can detect timeout post-execution
		assert.Equal(t, 408, w.Code, "Should return timeout status even when handler ignores context")
		assert.Greater(t, elapsed, 150*time.Millisecond, "Handler should run to completion (limitation)")

		// Verify timeout was properly detected and response replaced
		assert.Contains(t, w.Body.String(), "request timeout")
		assert.Equal(t, "true", w.Header().Get("X-Timeout"))

		t.Logf("✓ Timeout properly detected post-execution (expected serial behavior)")
	})

	t.Run("CPU intensive handler without context checking - post-execution timeout", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(30 * time.Millisecond)))
		r.Use(chain.Build())

		r.GET("/cpu-intensive", func(c *gin.Context) {
			// CPU intensive work that doesn't yield or check context
			// Use sleep to simulate work that takes longer than timeout
			time.Sleep(50 * time.Millisecond) // Longer than 30ms timeout

			sum := 0
			for i := 0; i < 10000; i++ {
				sum += i
				// No context checking - handler ignores timeout
			}
			c.JSON(200, gin.H{"result": sum})
		})

		start := time.Now()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/cpu-intensive", nil)
		r.ServeHTTP(w, req)
		elapsed := time.Since(start)

		t.Logf("CPU intensive request completed in %v", elapsed)
		t.Logf("Response code: %d", w.Code)

		// Serial implementation detects timeout after CPU work completes
		assert.Equal(t, 408, w.Code, "Should detect timeout after CPU work")
		assert.Contains(t, w.Body.String(), "request timeout")
		assert.Greater(t, elapsed, 40*time.Millisecond, "Handler should run longer than timeout")

		t.Log("✓ CPU intensive handler timed out properly (post-execution)")
	})

	t.Run("database simulation - cooperative timeout handling", func(t *testing.T) {
		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(80 * time.Millisecond)))
		r.Use(chain.Build())

		r.GET("/slow-db", func(c *gin.Context) {
			// Simulate database operation that cooperates with context
			ctx := c.Request.Context()

			// Simulate multiple DB calls with context checking
			for i := 0; i < 5; i++ {
				// Check context before each operation (good practice)
				select {
				case <-ctx.Done():
					t.Logf("DB operation %d: context cancelled, stopping early", i+1)
					return // Exit early when context is cancelled
				case <-time.After(30 * time.Millisecond):
					// Simulate DB work
					t.Logf("DB operation %d completed", i+1)
				}
			}

			c.JSON(200, gin.H{"data": "database result"})
		})

		start := time.Now()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/slow-db", nil)
		r.ServeHTTP(w, req)
		elapsed := time.Since(start)

		t.Logf("Database request completed in %v", elapsed)
		t.Logf("Response code: %d", w.Code)

		// With cooperative handler that checks context, timeout works properly
		assert.Equal(t, 408, w.Code, "Should timeout with cooperative handler")
		assert.Less(t, elapsed, 120*time.Millisecond, "Should timeout relatively quickly")
		assert.Contains(t, w.Body.String(), "request timeout")

		t.Log("✓ Database handler timed out properly with context cooperation")
	})
}

// TestTimeoutRaceConditions tests potential race conditions at timeout boundaries
func TestTimeoutRaceConditions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("race between normal completion and timeout", func(t *testing.T) {
		// Test handlers that complete right at the timeout boundary
		// This can cause race conditions between normal flush and timeout response

		r := gin.New()
		chain := NewChain().Use(Timeout(WithTimeout(50 * time.Millisecond)))
		r.Use(chain.Build())

		var raceConditions int
		var normalCompletions int
		var timeouts int

		r.GET("/race", func(c *gin.Context) {
			// Sleep for almost exactly the timeout duration
			time.Sleep(45 * time.Millisecond) // Just under 50ms timeout

			// Add some jitter to create race conditions
			jitter := time.Duration(rand.Intn(10)) * time.Millisecond
			time.Sleep(jitter)

			c.JSON(200, gin.H{"message": "completed"})
		})

		// Run many requests to detect race conditions
		for i := 0; i < 50; i++ {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/race", nil)
			r.ServeHTTP(w, req)

			switch w.Code {
			case 200:
				normalCompletions++
			case 408:
				timeouts++
			default:
				raceConditions++
				t.Logf("Unexpected response code: %d (possible race condition)", w.Code)
			}
		}

		t.Logf("Results: Normal=%d, Timeouts=%d, Race conditions=%d",
			normalCompletions, timeouts, raceConditions)

		if raceConditions > 0 {
			t.Log("✗ Detected race conditions at timeout boundary")
		}
	})

	t.Run("double write potential", func(t *testing.T) {
		r := gin.New()

		// Custom middleware to detect double writes
		var writeCount int
		var writeMutex sync.Mutex

		r.Use(func(c *gin.Context) {
			originalWriter := c.Writer

			// Wrap writer to count writes
			c.Writer = &countingWriter{
				ResponseWriter: originalWriter,
				onWrite: func() {
					writeMutex.Lock()
					writeCount++
					if writeCount > 1 {
						t.Log("✗ Detected multiple writes - potential double write issue")
					}
					writeMutex.Unlock()
				},
			}

			c.Next()
		})

		chain := NewChain().Use(Timeout(WithTimeout(30 * time.Millisecond)))
		r.Use(chain.Build())

		r.GET("/double-write", func(c *gin.Context) {
			// Handler that might complete right as timeout occurs
			time.Sleep(25 * time.Millisecond)

			// Write response
			c.Header("X-Handler", "completed")
			c.JSON(200, gin.H{"status": "ok"})

			// Add a small delay after writing to increase race window
			time.Sleep(10 * time.Millisecond)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/double-write", nil)

		writeCount = 0
		r.ServeHTTP(w, req)

		writeMutex.Lock()
		finalWriteCount := writeCount
		writeMutex.Unlock()

		t.Logf("Total writes detected: %d", finalWriteCount)
		t.Logf("Response code: %d", w.Code)

		// In a properly implemented timeout, there should be exactly one write
		// Either the handler completes and writes, or timeout occurs and writes
		// Never both (which would be a race condition)
	})
}

// countingWriter wraps ResponseWriter to count write operations
type countingWriter struct {
	gin.ResponseWriter
	onWrite func()
}

func (w *countingWriter) Write(data []byte) (int, error) {
	w.onWrite()
	return w.ResponseWriter.Write(data)
}

func (w *countingWriter) WriteHeader(statusCode int) {
	w.onWrite()
	w.ResponseWriter.WriteHeader(statusCode)
}

// TestTimeoutStillWorks verifies that timeout functionality still works after the fix
func TestTimeoutStillWorks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("timeout still works with chain fix", func(t *testing.T) {
		var middlewareExecuted = make(map[string]bool)

		trackingMiddleware := func(name string) Middleware {
			return func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					middlewareExecuted[name] = true
					next(c)
				}
			}
		}

		r := gin.New()
		chain := NewChain().
			Use(trackingMiddleware("Before-Timeout")).
			Use(Timeout(WithTimeout(50 * time.Millisecond))). // Short timeout
			Use(trackingMiddleware("After-Timeout"))          // This should execute but may not complete due to timeout

		r.Use(chain.Build())
		r.GET("/test", func(c *gin.Context) {
			select {
			case <-time.After(100 * time.Millisecond): // Longer than timeout
				c.JSON(200, gin.H{"message": "success"})
			case <-c.Request.Context().Done():
				return // Context cancelled due to timeout
			}
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		// Verify timeout still works
		assert.Equal(t, 408, w.Code, "Should return timeout status")
		assert.Contains(t, w.Body.String(), "request timeout")
		assert.Equal(t, "true", w.Header().Get("X-Timeout"), "Should have timeout header")

		// Both middlewares should start executing (chain is fixed)
		assert.True(t, middlewareExecuted["Before-Timeout"], "Before-Timeout middleware should execute")
		assert.True(t, middlewareExecuted["After-Timeout"], "After-Timeout middleware should execute (chain fixed)")

		t.Logf("Middlewares executed: %+v", middlewareExecuted)
	})
}

// TestTimeoutHeaderPreservationComplete tests comprehensive timeout middleware header preservation using Chain
func TestTimeoutHeaderPreservationComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Chain: CORS headers preserved on timeout", func(t *testing.T) {
		router := gin.New()

		chain := NewChain().
			Use(CORS(WithAllowOrigins("https://example.com"), WithAllowCredentials(true))).
			Use(Timeout(WithTimeout(10 * time.Millisecond)))

		router.GET("/api/data", chain.Build(), func(c *gin.Context) {
			c.Header("X-Business-Data", "important")
			time.Sleep(50 * time.Millisecond) // Will timeout
			c.JSON(http.StatusOK, gin.H{"data": "success"})
		})

		req := httptest.NewRequest("GET", "/api/data", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should timeout
		assert.Equal(t, http.StatusRequestTimeout, w.Code)

		// CORS headers should be preserved (set before timeout middleware)
		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))

		// Business headers should be preserved (now fixed!)
		assert.Equal(t, "important", w.Header().Get("X-Business-Data"))

		// Timeout marker should be present
		assert.Equal(t, "true", w.Header().Get("X-Timeout"))
	})

	t.Run("Chain: Trace and monitoring headers preserved", func(t *testing.T) {
		router := gin.New()

		chain := NewChain().
			Use(func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					// Trace middleware
					c.Header("X-Trace-ID", "trace-abc123")
					c.Header("X-Request-ID", "req-xyz789")
					next(c)
				}
			}).
			Use(Timeout(WithTimeout(10 * time.Millisecond))).
			Use(func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					// Monitoring middleware after timeout
					c.Header("X-Response-Time", "fast")
					next(c)
				}
			})

		router.POST("/api/process", chain.Build(), func(c *gin.Context) {
			// Business logic sets additional headers
			c.Header("X-Processing-Status", "started")
			c.Header("Cache-Control", "no-cache, must-revalidate")

			time.Sleep(50 * time.Millisecond) // Will timeout
			c.JSON(http.StatusOK, gin.H{"result": "processed"})
		})

		req := httptest.NewRequest("POST", "/api/process", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusRequestTimeout, w.Code)

		// Headers set before timeout middleware should be preserved
		assert.Equal(t, "trace-abc123", w.Header().Get("X-Trace-ID"))
		assert.Equal(t, "req-xyz789", w.Header().Get("X-Request-ID"))

		// Headers set after timeout middleware should be preserved (fixed!)
		assert.Equal(t, "fast", w.Header().Get("X-Response-Time"))

		// Business headers should be preserved (fixed!)
		assert.Equal(t, "started", w.Header().Get("X-Processing-Status"))
		assert.Equal(t, "no-cache, must-revalidate", w.Header().Get("Cache-Control"))
	})

	t.Run("Chain: Security headers preserved in timeout scenario", func(t *testing.T) {
		router := gin.New()

		chain := NewChain().
			Use(func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					// Security middleware
					c.Header("X-Frame-Options", "DENY")
					c.Header("X-Content-Type-Options", "nosniff")
					c.Header("X-XSS-Protection", "1; mode=block")
					c.Header("Strict-Transport-Security", "max-age=31536000")
					next(c)
				}
			}).
			Use(Timeout(WithTimeout(10 * time.Millisecond)))

		router.GET("/secure-data", chain.Build(), func(c *gin.Context) {
			// API sets response headers
			c.Header("X-API-Version", "v2.1")
			c.Header("X-Rate-Limit-Remaining", "99")

			time.Sleep(50 * time.Millisecond) // Will timeout
			c.JSON(http.StatusOK, gin.H{"secure": true})
		})

		req := httptest.NewRequest("GET", "/secure-data", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusRequestTimeout, w.Code)

		// Security headers should all be preserved
		assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
		assert.Equal(t, "1; mode=block", w.Header().Get("X-XSS-Protection"))
		assert.Equal(t, "max-age=31536000", w.Header().Get("Strict-Transport-Security"))

		// API headers should be preserved (fixed!)
		assert.Equal(t, "v2.1", w.Header().Get("X-API-Version"))
		assert.Equal(t, "99", w.Header().Get("X-Rate-Limit-Remaining"))
	})

	t.Run("Chain: Complex real-world scenario", func(t *testing.T) {
		router := gin.New()

		// Simulate a real-world middleware chain
		chain := NewChain().
			// CORS for cross-origin requests
			Use(CORS(
				WithAllowOrigins("https://app.example.com", "https://admin.example.com"),
				WithAllowCredentials(true),
				WithExposeHeaders("X-Total-Count", "X-Rate-Limit-Remaining"),
			)).
			// Request tracking
			Use(func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					c.Header("X-Request-ID", "req-"+c.GetHeader("X-Client-ID"))
					c.Header("X-Trace-ID", "trace-"+time.Now().Format("20060102150405"))
					next(c)
				}
			}).
			// Rate limiting info
			Use(func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					c.Header("X-Rate-Limit-Limit", "1000")
					c.Header("X-Rate-Limit-Remaining", "999")
					next(c)
				}
			}).
			// Timeout middleware
			Use(Timeout(WithTimeout(10*time.Millisecond), WithTimeoutMessage("服务繁忙，请稍后重试"))).
			// Security headers (after timeout)
			Use(func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					c.Header("X-Frame-Options", "SAMEORIGIN")
					c.Header("Content-Security-Policy", "default-src 'self'")
					next(c)
				}
			})

		router.GET("/api/users/:id", chain.Build(), func(c *gin.Context) {
			// Business logic headers
			c.Header("X-User-Role", "admin")
			c.Header("X-Total-Count", "42")
			c.Header("Cache-Control", "private, max-age=300")
			c.Header("ETag", `"user-123-v2"`)

			// Simulate slow database query
			time.Sleep(50 * time.Millisecond)

			c.JSON(http.StatusOK, gin.H{
				"id":   c.Param("id"),
				"name": "John Doe",
			})
		})

		req := httptest.NewRequest("GET", "/api/users/123", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("X-Client-ID", "mobile-app")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Verify timeout response
		assert.Equal(t, http.StatusRequestTimeout, w.Code)

		// Check timeout response content
		assert.Contains(t, w.Body.String(), "服务繁忙，请稍后重试")

		// CORS headers should be preserved
		assert.Equal(t, "https://app.example.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
		assert.Equal(t, "X-Total-Count, X-Rate-Limit-Remaining", w.Header().Get("Access-Control-Expose-Headers"))

		// Tracking headers should be preserved
		assert.Equal(t, "req-mobile-app", w.Header().Get("X-Request-ID"))
		assert.Contains(t, w.Header().Get("X-Trace-ID"), "trace-")

		// Rate limiting headers should be preserved
		assert.Equal(t, "1000", w.Header().Get("X-Rate-Limit-Limit"))
		assert.Equal(t, "999", w.Header().Get("X-Rate-Limit-Remaining"))

		// Security headers (after timeout) should be preserved
		assert.Equal(t, "SAMEORIGIN", w.Header().Get("X-Frame-Options"))
		assert.Equal(t, "default-src 'self'", w.Header().Get("Content-Security-Policy"))

		// Business headers should be preserved
		assert.Equal(t, "admin", w.Header().Get("X-User-Role"))
		assert.Equal(t, "42", w.Header().Get("X-Total-Count"))
		assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))
		assert.Equal(t, `"user-123-v2"`, w.Header().Get("ETag"))

		// Timeout marker
		assert.Equal(t, "true", w.Header().Get("X-Timeout"))
	})

	t.Run("Chain: Custom timeout response preserves headers", func(t *testing.T) {
		router := gin.New()

		chain := NewChain().
			Use(func(next gin.HandlerFunc) gin.HandlerFunc {
				return func(c *gin.Context) {
					c.Header("X-Service", "user-service")
					c.Header("X-Version", "1.2.3")
					next(c)
				}
			}).
			Use(Timeout(
				WithTimeout(10*time.Millisecond),
				WithTimeoutResponse(gin.H{
					"error":       "timeout",
					"code":        "TIMEOUT_ERROR",
					"message":     "请求超时，请重试",
					"retry_after": 5,
				}),
			))

		router.PUT("/api/update", chain.Build(), func(c *gin.Context) {
			c.Header("X-Operation", "update")
			time.Sleep(50 * time.Millisecond)
			c.JSON(http.StatusOK, gin.H{"updated": true})
		})

		req := httptest.NewRequest("PUT", "/api/update", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusRequestTimeout, w.Code)

		// Service headers should be preserved
		assert.Equal(t, "user-service", w.Header().Get("X-Service"))
		assert.Equal(t, "1.2.3", w.Header().Get("X-Version"))

		// Operation header should be preserved
		assert.Equal(t, "update", w.Header().Get("X-Operation"))

		// Custom timeout response should be returned
		assert.Contains(t, w.Body.String(), "TIMEOUT_ERROR")
		assert.Contains(t, w.Body.String(), "请求超时，请重试")
		assert.Contains(t, w.Body.String(), "retry_after")
	})
}
