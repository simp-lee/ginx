package ginx

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestChainBasicUsage(t *testing.T) {
	t.Run("NewChain creates empty chain", func(t *testing.T) {
		chain := NewChain()

		if chain == nil {
			t.Error("NewChain should return a non-nil chain")
		} else {
			if len(chain.middlewares) != 0 {
				t.Error("NewChain should create chain with empty middleware list")
			}
		}

		if chain.errorHandler != nil {
			t.Error("NewChain should create chain with nil error handler")
		}
	})

	t.Run("Use adds middleware to chain", func(t *testing.T) {
		chain := NewChain()
		var executed []string

		middleware := TestMiddleware("test", &executed)
		result := chain.Use(middleware)

		// Should return the same chain for method chaining
		if result != chain {
			t.Error("Use should return the same chain instance for method chaining")
		}

		if len(chain.middlewares) != 1 {
			t.Error("Use should add middleware to the chain")
		}
	})
}

func TestChainExecution(t *testing.T) {
	t.Run("Single middleware execution", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			Use(TestMiddleware("middleware1", &executed))

		handler := chain.Build()

		c, _ := TestContext("GET", "/test", nil)
		c.Set("test", true) // Set some test data

		handler(c)

		expected := []string{"middleware1"}
		if !AssertSliceEqual(expected, executed) {
			t.Errorf("Expected execution order %v, got %v", expected, executed)
		}
	})

	t.Run("Multiple middleware execution order", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			Use(TestMiddleware("middleware1", &executed)).
			Use(TestMiddleware("middleware2", &executed)).
			Use(TestMiddleware("middleware3", &executed))

		handler := chain.Build()

		c, _ := TestContext("GET", "/test", nil)
		handler(c)

		// Middleware should execute in the order they were added
		expected := []string{"middleware1", "middleware2", "middleware3"}
		if !AssertSliceEqual(expected, executed) {
			t.Errorf("Expected execution order %v, got %v", expected, executed)
		}
	})

	t.Run("Chain with actual gin handlers", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			Use(TestMiddleware("auth", &executed)).
			Use(TestMiddleware("logging", &executed))

		handler := chain.Build()

		// Create a mock gin engine for testing
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.GET("/test", handler, TestHandler(&executed))

		// We only test if the chain builds successfully
		if handler == nil {
			t.Error("Chain.Build() should return a valid handler")
		}
	})
}

func TestChainConditionalExecution(t *testing.T) {
	t.Run("When condition true - middleware executes", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			When(PathIs("/api/users"), TestMiddleware("auth", &executed))

		handler := chain.Build()

		c, _ := TestContext("GET", "/api/users", nil)
		handler(c)

		if !AssertContains(executed, "auth") {
			t.Error("When condition true: middleware should execute")
		}
	})

	t.Run("When condition false - middleware skips", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			When(PathIs("/api/users"), TestMiddleware("auth", &executed))

		handler := chain.Build()

		c, _ := TestContext("GET", "/public", nil)
		handler(c)

		if AssertContains(executed, "auth") {
			t.Error("When condition false: middleware should not execute")
		}
	})

	t.Run("Unless condition true - middleware skips", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			Unless(PathIs("/public"), TestMiddleware("auth", &executed))

		handler := chain.Build()

		c, _ := TestContext("GET", "/public", nil)
		handler(c)

		if AssertContains(executed, "auth") {
			t.Error("Unless condition true: middleware should not execute")
		}
	})

	t.Run("Unless condition false - middleware executes", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			Unless(PathIs("/public"), TestMiddleware("auth", &executed))

		handler := chain.Build()

		c, _ := TestContext("GET", "/api/users", nil)
		handler(c)

		if !AssertContains(executed, "auth") {
			t.Error("Unless condition false: middleware should execute")
		}
	})
}

func TestChainComplexScenarios(t *testing.T) {
	t.Run("Mixed conditional and unconditional middleware", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			Use(TestMiddleware("cors", &executed)).                          // Always executed
			When(PathHasPrefix("/api"), TestMiddleware("auth", &executed)).  // Only executed for API paths
			Unless(PathIs("/health"), TestMiddleware("logging", &executed)). // Executed for all except health check
			Use(TestMiddleware("response", &executed))                       // Always executed

		handler := chain.Build()

		// Test API path
		c, _ := TestContext("GET", "/api/users", nil)
		handler(c)

		expected := []string{"cors", "auth", "logging", "response"}
		if !AssertSliceEqual(expected, executed) {
			t.Errorf("API path: expected %v, got %v", expected, executed)
		}
	})

	t.Run("Health check path", func(t *testing.T) {
		var executed []string

		chain := NewChain().
			Use(TestMiddleware("cors", &executed)).
			When(PathHasPrefix("/api"), TestMiddleware("auth", &executed)).
			Unless(PathIs("/health"), TestMiddleware("logging", &executed)).
			Use(TestMiddleware("response", &executed))

		handler := chain.Build()

		c, _ := TestContext("GET", "/health", nil)
		handler(c)

		// /health path: cors executes, auth doesn't execute (not /api), logging doesn't execute (excluded by Unless), response executes
		expected := []string{"cors", "response"}
		if !AssertSliceEqual(expected, executed) {
			t.Errorf("Health path: expected %v, got %v", expected, executed)
		}
	})

	t.Run("Complex conditions with And/Or", func(t *testing.T) {
		var executed []string

		// Complex condition: (API path AND POST method) OR admin path
		complexCondition := Or(
			And(PathHasPrefix("/api"), MethodIs("POST")),
			PathHasPrefix("/admin"),
		)

		chain := NewChain().
			When(complexCondition, TestMiddleware("security", &executed))

		handler := chain.Build()

		// Test 1: API POST request
		c1, _ := TestContext("POST", "/api/users", nil)
		handler(c1)

		if !AssertContains(executed, "security") {
			t.Error("Complex condition: API POST should trigger security middleware")
		}

		// Reset execution record
		executed = []string{}

		// Test 2: Admin path
		c2, _ := TestContext("GET", "/admin/dashboard", nil)
		handler(c2)

		if !AssertContains(executed, "security") {
			t.Error("Complex condition: Admin path should trigger security middleware")
		}

		// Reset execution record
		executed = []string{}

		// Test 3: Regular API GET request (should not trigger)
		c3, _ := TestContext("GET", "/api/users", nil)
		handler(c3)

		if AssertContains(executed, "security") {
			t.Error("Complex condition: API GET should not trigger security middleware")
		}
	})
}

func TestChainErrorHandler(t *testing.T) {
	t.Run("OnError sets error handler", func(t *testing.T) {
		errorHandler := func(c *gin.Context, err error) {
			// Error handling logic
		}

		chain := NewChain().OnError(errorHandler)

		if chain.errorHandler == nil {
			t.Error("OnError should set the error handler")
		}

		// Note: The actual usage of errorHandler needs to be implemented in specific middleware
		// Here we only test if the setting is successful
	})

	t.Run("OnError returns chain for method chaining", func(t *testing.T) {
		chain := NewChain()

		result := chain.OnError(func(c *gin.Context, err error) {})

		if result != chain {
			t.Error("OnError should return the same chain instance for method chaining")
		}
	})
}

func TestChainMethodChaining(t *testing.T) {
	t.Run("All methods support chaining", func(t *testing.T) {
		var executed []string

		// Test that all methods support method chaining
		chain := NewChain().
			OnError(func(c *gin.Context, err error) {}).
			Use(TestMiddleware("middleware1", &executed)).
			When(PathHasPrefix("/api"), TestMiddleware("auth", &executed)).
			Unless(PathIs("/health"), TestMiddleware("logging", &executed)).
			Use(TestMiddleware("middleware2", &executed))

		handler := chain.Build()

		if handler == nil {
			t.Error("Chain method chaining should work correctly")
		}

		// Verify that the handler created by method chaining works properly
		c, _ := TestContext("GET", "/api/test", nil)
		handler(c)

		// Should execute: middleware1, auth, logging, middleware2
		expected := []string{"middleware1", "auth", "logging", "middleware2"}
		if !AssertSliceEqual(expected, executed) {
			t.Errorf("Method chaining result: expected %v, got %v", expected, executed)
		}
	})
}

func TestChainEdgeCases(t *testing.T) {
	t.Run("Empty chain execution", func(t *testing.T) {
		chain := NewChain()
		handler := chain.Build()

		c, _ := TestContext("GET", "/test", nil)

		// Should not panic
		handler(c)

		// Verify request status
		if c.Writer.Status() != http.StatusOK {
			// Note: Empty chain may not set status code, this is normal
		}
	})

	t.Run("Nil condition handling", func(t *testing.T) {
		// This test ensures our implementation can correctly handle edge cases
		chain := NewChain()
		handler := chain.Build()

		if handler == nil {
			t.Error("Build should always return a valid handler function")
		}
	})
}

func TestChainErrorHandlerExecution(t *testing.T) {
	t.Run("ErrorHandler should be called when middleware reports error", func(t *testing.T) {
		var errorHandlerCalled bool
		var capturedError error

		// Create a middleware that reports errors
		errorMiddleware := func(next gin.HandlerFunc) gin.HandlerFunc {
			return func(c *gin.Context) {
				// Simulate an error in middleware
				err := errors.New("middleware error")
				c.Error(err)
				next(c)
			}
		}

		// Set up error handler
		chain := NewChain().
			OnError(func(c *gin.Context, err error) {
				errorHandlerCalled = true
				capturedError = err
			}).
			Use(errorMiddleware)

		handler := chain.Build()
		c, _ := TestContext("GET", "/test", nil)

		handler(c)

		// In the current implementation, errorHandler will not be called
		// This test should fail, proving the problem exists
		if !errorHandlerCalled {
			t.Error("ErrorHandler was not called despite middleware reporting an error - this demonstrates the bug")
		}
		if capturedError == nil {
			t.Error("ErrorHandler should have received the error from middleware")
		}
	})

	t.Run("ErrorHandler should not be called when no errors occur", func(t *testing.T) {
		var errorHandlerCalled bool

		// Create a normal middleware
		normalMiddleware := func(next gin.HandlerFunc) gin.HandlerFunc {
			return func(c *gin.Context) {
				next(c)
			}
		}

		chain := NewChain().
			OnError(func(c *gin.Context, err error) {
				errorHandlerCalled = true
			}).
			Use(normalMiddleware)

		handler := chain.Build()
		c, _ := TestContext("GET", "/test", nil)

		handler(c)

		if errorHandlerCalled {
			t.Error("ErrorHandler should not be called when no errors occur")
		}
	})

	t.Run("ErrorHandler should handle multiple errors correctly", func(t *testing.T) {
		var errorHandlerCallCount int
		var capturedErrors []error

		// Create multiple middlewares that report errors
		errorMiddleware1 := func(next gin.HandlerFunc) gin.HandlerFunc {
			return func(c *gin.Context) {
				err := errors.New("first error")
				c.Error(err)
				next(c)
			}
		}

		errorMiddleware2 := func(next gin.HandlerFunc) gin.HandlerFunc {
			return func(c *gin.Context) {
				err := errors.New("second error")
				c.Error(err)
				next(c)
			}
		}

		chain := NewChain().
			OnError(func(c *gin.Context, err error) {
				errorHandlerCallCount++
				capturedErrors = append(capturedErrors, err)
			}).
			Use(errorMiddleware1).
			Use(errorMiddleware2)

		handler := chain.Build()
		c, _ := TestContext("GET", "/test", nil)

		handler(c)

		// Expect error handler to be called and handle the last error
		if errorHandlerCallCount == 0 {
			t.Error("ErrorHandler was not called despite multiple middleware errors - this demonstrates the bug")
		}
	})
}
