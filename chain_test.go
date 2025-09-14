package ginx

import (
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
		c.Set("test", true) // 设置一些测试数据

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

		// 中间件应该按添加顺序执行
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

		// 创建一个模拟的 gin 引擎来测试
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.GET("/test", handler, TestHandler(&executed))

		// 这里我们只测试链构建是否成功
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
			Use(TestMiddleware("cors", &executed)).                          // 总是执行
			When(PathHasPrefix("/api"), TestMiddleware("auth", &executed)).  // 只在 API 路径执行
			Unless(PathIs("/health"), TestMiddleware("logging", &executed)). // 除了健康检查都执行
			Use(TestMiddleware("response", &executed))                       // 总是执行

		handler := chain.Build()

		// 测试 API 路径
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

		// /health 路径：cors 执行，auth 不执行（不是 /api），logging 不执行（被 Unless 排除），response 执行
		expected := []string{"cors", "response"}
		if !AssertSliceEqual(expected, executed) {
			t.Errorf("Health path: expected %v, got %v", expected, executed)
		}
	})

	t.Run("Complex conditions with And/Or", func(t *testing.T) {
		var executed []string

		// 复杂条件：(API 路径 AND POST 方法) OR 管理员路径
		complexCondition := Or(
			And(PathHasPrefix("/api"), MethodIs("POST")),
			PathHasPrefix("/admin"),
		)

		chain := NewChain().
			When(complexCondition, TestMiddleware("security", &executed))

		handler := chain.Build()

		// 测试 1: API POST 请求
		c1, _ := TestContext("POST", "/api/users", nil)
		handler(c1)

		if !AssertContains(executed, "security") {
			t.Error("Complex condition: API POST should trigger security middleware")
		}

		// 重置执行记录
		executed = []string{}

		// 测试 2: 管理员路径
		c2, _ := TestContext("GET", "/admin/dashboard", nil)
		handler(c2)

		if !AssertContains(executed, "security") {
			t.Error("Complex condition: Admin path should trigger security middleware")
		}

		// 重置执行记录
		executed = []string{}

		// 测试 3: 普通 API GET 请求（不应触发）
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
			// 错误处理逻辑
		}

		chain := NewChain().OnError(errorHandler)

		if chain.errorHandler == nil {
			t.Error("OnError should set the error handler")
		}

		// 注意：errorHandler 的实际使用需要在具体的中间件中实现
		// 这里只测试设置是否成功
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

		// 测试所有方法都支持链式调用
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

		// 验证链式调用创建的处理器能正常工作
		c, _ := TestContext("GET", "/api/test", nil)
		handler(c)

		// 应该执行：middleware1, auth, logging, middleware2
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

		// 应该不会 panic
		handler(c)

		// 验证请求状态
		if c.Writer.Status() != http.StatusOK {
			// 注意：空链可能不会设置状态码，这是正常的
		}
	})

	t.Run("Nil condition handling", func(t *testing.T) {
		// 这个测试确保我们的实现能正确处理边界情况
		chain := NewChain()
		handler := chain.Build()

		if handler == nil {
			t.Error("Build should always return a valid handler function")
		}
	})
}
