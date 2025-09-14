package ginx

import (
	"net/http"
	"net/http/httptest"
	"slices"

	"github.com/gin-gonic/gin"
)

// 测试工具和辅助函数

// TestContext 创建用于测试的 gin.Context
func TestContext(method, path string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(method, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	c.Request = req

	// Set the remote address for ClientIP() to work properly
	c.Request.RemoteAddr = "192.0.2.1:1234"

	return c, w
}

// TestMiddleware 创建用于测试的中间件，记录执行状态
func TestMiddleware(name string, executed *[]string) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			*executed = append(*executed, name)
			next(c)
		}
	}
}

// TestHandler 创建用于测试的处理器
func TestHandler(executed *[]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		*executed = append(*executed, "handler")
		c.Status(http.StatusOK)
	}
}

// AssertContains 检查切片是否包含指定元素
func AssertContains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

// AssertEqual 检查两个值是否相等
func AssertEqual(expected, actual interface{}) bool {
	return expected == actual
}

// AssertSliceEqual 检查两个字符串切片是否相等
func AssertSliceEqual(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return false
		}
	}
	return true
}
