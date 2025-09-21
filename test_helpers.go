package ginx

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"

	"github.com/gin-gonic/gin"
)

// Test utilities and helper functions

var ginTestModeOnce sync.Once

// TestContext creates a gin.Context for testing
func TestContext(method, path string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	// Use sync.Once to ensure setting only once, avoiding concurrent race conditions
	ginTestModeOnce.Do(func() {
		gin.SetMode(gin.TestMode)
	})

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

// TestMiddleware creates middleware for testing that records execution state
func TestMiddleware(name string, executed *[]string) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			*executed = append(*executed, name)
			next(c)
		}
	}
}

// TestHandler creates a handler for testing
func TestHandler(executed *[]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		*executed = append(*executed, "handler")
		c.Status(http.StatusOK)
	}
}

// AssertContains checks if slice contains the specified element
func AssertContains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

// AssertEqual checks if two values are equal
func AssertEqual(expected, actual interface{}) bool {
	return expected == actual
}

// AssertSliceEqual checks if two string slices are equal
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
