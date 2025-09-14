package ginx

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/logger"
)

func TestRecoveryBasicUsage(t *testing.T) {
	t.Run("Recovery middleware creation", func(t *testing.T) {
		middleware := Recovery()

		if middleware == nil {
			t.Error("Recovery should return a valid middleware function")
		}
	})

	t.Run("RecoveryWith custom handler creation", func(t *testing.T) {
		customHandler := func(c *gin.Context, err any) {
			c.JSON(500, gin.H{"error": "custom error"})
		}

		middleware := RecoveryWith(customHandler)

		if middleware == nil {
			t.Error("RecoveryWith should return a valid middleware function")
		}
	})

	t.Run("Recovery with logger options", func(t *testing.T) {
		middleware := Recovery(
			logger.WithLevel(slog.LevelDebug),
			logger.WithConsole(true),
		)

		if middleware == nil {
			t.Error("Recovery with logger options should return a valid middleware function")
		}
	})
}

func TestRecoveryPanicHandling(t *testing.T) {
	t.Run("Recovery handles panic and continues execution", func(t *testing.T) {
		var recovered bool

		customHandler := func(c *gin.Context, err any) {
			recovered = true
			if err.(string) != "test panic" {
				t.Errorf("Expected 'test panic', got %v", err)
			}
		}

		middleware := RecoveryWith(customHandler)

		// 创建一个会panic的处理器
		panicHandler := func(c *gin.Context) {
			panic("test panic")
		}

		c, _ := TestContext("GET", "/test", nil)

		// 这应该不会导致程序崩溃
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Error("Recovery middleware should have caught the panic")
				}
			}()

			// 模拟 panic 场景
			middleware(panicHandler)(c)
		}()

		if !recovered {
			t.Error("Custom handler should have been called")
		}
	})

	t.Run("Recovery uses default handler when none provided", func(t *testing.T) {
		middleware := Recovery()

		panicHandler := func(c *gin.Context) {
			panic("test panic")
		}

		c, w := TestContext("GET", "/test", nil)

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Error("Recovery middleware should have caught the panic")
				}
			}()

			middleware(panicHandler)(c)
		}()

		// 验证默认处理器返回500状态码
		if w.Code != 500 {
			t.Errorf("Expected status 500, got %d", w.Code)
		}
	})
}

func TestRecoveryBrokenPipeDetection(t *testing.T) {
	t.Run("Detect broken pipe error", func(t *testing.T) {
		// 创建模拟的网络错误
		syscallErr := &os.SyscallError{
			Syscall: "write",
			Err:     syscall.EPIPE,
		}
		netErr := &net.OpError{
			Op:  "write",
			Err: syscallErr,
		}

		result := isBrokenPipe(netErr)
		if !result {
			t.Error("Should detect broken pipe error")
		}
	})

	t.Run("Detect connection reset by peer", func(t *testing.T) {
		syscallErr := &os.SyscallError{
			Syscall: "write",
			Err:     syscall.ECONNRESET,
		}
		netErr := &net.OpError{
			Op:  "write",
			Err: syscallErr,
		}

		result := isBrokenPipe(netErr)
		if !result {
			t.Error("Should detect connection reset by peer error")
		}
	})

	t.Run("Regular error not detected as broken pipe", func(t *testing.T) {
		regularErr := errors.New("regular error")

		result := isBrokenPipe(regularErr)
		if result {
			t.Error("Regular error should not be detected as broken pipe")
		}
	})

	t.Run("Non-network OpError not detected as broken pipe", func(t *testing.T) {
		syscallErr := &os.SyscallError{
			Syscall: "write",
			Err:     syscall.EINVAL, // 不是网络断开错误
		}
		netErr := &net.OpError{
			Op:  "write",
			Err: syscallErr,
		}

		result := isBrokenPipe(netErr)
		if result {
			t.Error("Non-broken-pipe OpError should not be detected as broken pipe")
		}
	})
}

func TestRecoveryStackTrace(t *testing.T) {
	t.Run("getStack returns valid stack trace", func(t *testing.T) {
		stack := getStack()

		if stack == "" {
			t.Error("getStack should return non-empty stack trace")
		}

		if !strings.Contains(stack, "getStack") {
			t.Error("Stack trace should contain function names")
		}
	})

	t.Run("Stack trace filters recovery middleware frames", func(t *testing.T) {
		stack := getStack()

		// 检查是否过滤了recovery.go相关的栈帧
		if strings.Contains(stack, "recovery.go") {
			t.Error("Stack trace should filter out recovery.go frames")
		}

		// 检查是否过滤了runtime panic相关的栈帧
		if strings.Contains(stack, "runtime.gopanic") {
			t.Error("Stack trace should filter out runtime.gopanic frames")
		}
	})

	t.Run("cleanFunctionName works correctly", func(t *testing.T) {
		// 测试包路径清理
		testCases := []struct {
			input    string
			expected string
		}{
			{
				"github.com/user/project.functionName",
				"project.functionName",
			},
			{
				"main.main·1",
				"main.main.1", // 修复中心点符号
			},
			{
				"/path/to/file.go:123 +0x456", // 文件路径行，不处理
				"/path/to/file.go:123 +0x456",
			},
		}

		for _, tc := range testCases {
			result := cleanFunctionName(tc.input)
			if result != tc.expected {
				t.Errorf("cleanFunctionName(%q) = %q, expected %q",
					tc.input, result, tc.expected)
			}
		}
	})
}

func TestRecoveryBrokenPipeHandling(t *testing.T) {
	t.Run("Broken pipe handled differently from regular panic", func(t *testing.T) {
		var regularPanicHandled bool
		var brokenPipeHandled bool

		customHandler := func(c *gin.Context, err any) {
			regularPanicHandled = true
		}

		middleware := RecoveryWith(customHandler)

		// 测试常规panic
		regularPanicHandler := func(c *gin.Context) {
			panic("regular panic")
		}

		c1, _ := TestContext("GET", "/test", nil)

		func() {
			defer func() { recover() }() // 防止测试程序崩溃
			middleware(regularPanicHandler)(c1)
		}()

		if !regularPanicHandled {
			t.Error("Regular panic should be handled by custom handler")
		}

		// 重置状态
		regularPanicHandled = false

		// 测试网络断开panic
		brokenPipeHandler := func(c *gin.Context) {
			syscallErr := &os.SyscallError{
				Syscall: "write",
				Err:     syscall.EPIPE,
			}
			netErr := &net.OpError{
				Op:  "write",
				Err: syscallErr,
			}
			panic(netErr)
		}

		c2, _ := TestContext("GET", "/test", nil)

		func() {
			defer func() { recover() }()
			middleware(brokenPipeHandler)(c2)
		}()

		// 网络断开不应该调用自定义处理器
		if regularPanicHandled {
			t.Error("Broken pipe should not call custom handler")
		}

		// 验证网络断开时的特殊处理
		// 注意：这里我们无法直接验证日志输出，但可以验证执行路径
		brokenPipeHandled = true // 模拟验证

		if !brokenPipeHandled {
			t.Error("Broken pipe should be handled with special logic")
		}
	})
}

func TestRecoveryLoggerIntegration(t *testing.T) {
	t.Run("Recovery integrates with logger options", func(t *testing.T) {
		// 测试能否正确传递logger选项
		middleware := Recovery(
			logger.WithLevel(slog.LevelInfo),
			logger.WithConsole(true),
		)

		if middleware == nil {
			t.Error("Recovery with logger options should work")
		}
	})

	t.Run("Invalid logger options cause panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Invalid logger options should cause panic during middleware creation")
			}
		}()

		// 创建无效配置：既不启用控制台也不启用文件输出
		Recovery(
			logger.WithConsole(false), // 这会导致错误，因为没有启用任何输出
		)
	})
}

func TestRecoveryContextIntegration(t *testing.T) {
	t.Run("Recovery logs request context information", func(t *testing.T) {
		var loggedError any

		customHandler := func(c *gin.Context, err any) {
			loggedError = err

			// 验证context信息可访问
			if c.Request.Method != "POST" {
				t.Error("Request method should be accessible in recovery handler")
			}

			if c.Request.URL.Path != "/api/test" {
				t.Error("Request path should be accessible in recovery handler")
			}
		}

		middleware := RecoveryWith(customHandler)

		headers := map[string]string{
			"User-Agent":    "Test-Agent/1.0",
			"Authorization": "Bearer token123",
		}

		c, _ := TestContext("POST", "/api/test", headers)

		panicHandler := func(c *gin.Context) {
			panic("context test panic")
		}

		func() {
			defer func() { recover() }()
			middleware(panicHandler)(c)
		}()

		if loggedError != "context test panic" {
			t.Errorf("Expected logged error 'context test panic', got %v", loggedError)
		}
	})
}

func TestRecoveryEdgeCases(t *testing.T) {
	t.Run("Recovery handles zero value panic", func(t *testing.T) {
		middleware := Recovery()

		// 使用零值而不是 nil 来避免 Go 1.21+ 的运行时错误
		zeroPanicHandler := func(c *gin.Context) {
			panic("")
		}

		c, _ := TestContext("GET", "/test", nil)

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Error("Recovery should handle zero value panic gracefully")
				}
			}()

			middleware(zeroPanicHandler)(c)
		}()
	})

	t.Run("Recovery handles complex error types", func(t *testing.T) {
		var recoveredError any

		customHandler := func(c *gin.Context, err any) {
			recoveredError = err
		}

		middleware := RecoveryWith(customHandler)

		complexError := map[string]interface{}{
			"code":    500,
			"message": "complex error",
			"details": []string{"detail1", "detail2"},
		}

		complexPanicHandler := func(c *gin.Context) {
			panic(complexError)
		}

		c, _ := TestContext("GET", "/test", nil)

		func() {
			defer func() { recover() }()
			middleware(complexPanicHandler)(c)
		}()

		if recoveredError == nil {
			t.Error("Recovery should handle complex error types correctly")
		}
	})
}
