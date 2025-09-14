package ginx

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestCORSBasicUsage(t *testing.T) {
	t.Run("CORS middleware creation", func(t *testing.T) {
		middleware := CORS(WithAllowOrigins("https://example.com"))

		if middleware == nil {
			t.Error("CORS should return a valid middleware function")
		}
	})

	t.Run("CORSDefault creates development middleware", func(t *testing.T) {
		middleware := CORSDefault()

		if middleware == nil {
			t.Error("CORSDefault should return a valid middleware function")
		}
	})

	t.Run("CORS with multiple options", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com", "https://app.example.com"),
			WithAllowMethods("GET", "POST", "PUT"),
			WithAllowHeaders("Content-Type", "Authorization"),
			WithAllowCredentials(true),
			WithMaxAge(24*time.Hour),
		)

		if middleware == nil {
			t.Error("CORS with multiple options should work")
		}
	})
}

func TestCORSSecurityValidation(t *testing.T) {
	t.Run("Wildcard with credentials should panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("CORS with wildcard origin and credentials should panic")
			} else {
				errorMsg := r.(string)
				if !strings.Contains(errorMsg, "security error") {
					t.Errorf("Expected security error message, got: %s", errorMsg)
				}
			}
		}()

		CORS(
			WithAllowOrigins("*"),
			WithAllowCredentials(true),
		)
	})

	t.Run("Explicit origins with credentials should work", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Explicit origins with credentials should not panic: %v", r)
			}
		}()

		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithAllowCredentials(true),
		)

		if middleware == nil {
			t.Error("Should create valid middleware")
		}
	})
}

func TestCORSPreflightRequests(t *testing.T) {
	t.Run("Valid preflight request", func(t *testing.T) {
		middleware := CORS(WithAllowOrigins("https://example.com"))

		headers := map[string]string{
			"Origin":                         "https://example.com",
			"Access-Control-Request-Method":  "POST",
			"Access-Control-Request-Headers": "Content-Type",
		}

		c, w := TestContext("OPTIONS", "/api/users", headers)

		var nextCalled bool
		next := func(c *gin.Context) {
			nextCalled = true
		}

		middleware(next)(c)

		// 预检请求不应该调用next
		if nextCalled {
			t.Error("Preflight request should not call next handler")
		}

		// 检查响应状态码
		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
		}

		// 检查CORS头部
		if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Error("Should set correct Allow-Origin header")
		}

		if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "POST") {
			t.Error("Should allow requested method")
		}
	})

	t.Run("Invalid origin in preflight", func(t *testing.T) {
		middleware := CORS(WithAllowOrigins("https://allowed.com"))

		headers := map[string]string{
			"Origin":                        "https://notallowed.com",
			"Access-Control-Request-Method": "POST",
		}

		c, w := TestContext("OPTIONS", "/api/users", headers)

		middleware(func(c *gin.Context) {})(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d for invalid origin, got %d",
				http.StatusForbidden, w.Code)
		}
	})

	t.Run("Invalid method in preflight", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithAllowMethods("GET", "POST"),
		)

		headers := map[string]string{
			"Origin":                        "https://example.com",
			"Access-Control-Request-Method": "DELETE", // Not allowed
		}

		c, w := TestContext("OPTIONS", "/api/users", headers)

		middleware(func(c *gin.Context) {})(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d for invalid method, got %d",
				http.StatusForbidden, w.Code)
		}
	})

	t.Run("Invalid headers in preflight", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithAllowHeaders("Content-Type"),
		)

		headers := map[string]string{
			"Origin":                         "https://example.com",
			"Access-Control-Request-Method":  "POST",
			"Access-Control-Request-Headers": "X-Custom-Header", // Not allowed
		}

		c, w := TestContext("OPTIONS", "/api/users", headers)

		middleware(func(c *gin.Context) {})(c)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d for invalid headers, got %d",
				http.StatusForbidden, w.Code)
		}
	})
}

func TestCORSActualRequests(t *testing.T) {
	t.Run("Valid actual request", func(t *testing.T) {
		middleware := CORS(WithAllowOrigins("https://example.com"))

		headers := map[string]string{
			"Origin": "https://example.com",
		}

		c, w := TestContext("GET", "/api/users", headers)

		var nextCalled bool
		next := func(c *gin.Context) {
			nextCalled = true
			c.Status(http.StatusOK)
		}

		middleware(next)(c)

		// 实际请求应该调用next
		if !nextCalled {
			t.Error("Actual request should call next handler")
		}

		// 检查CORS头部
		if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Error("Should set correct Allow-Origin header")
		}

		if w.Header().Get("Vary") != "Origin" {
			t.Error("Should set Vary: Origin header for specific origins")
		}
	})

	t.Run("Invalid origin in actual request", func(t *testing.T) {
		middleware := CORS(WithAllowOrigins("https://allowed.com"))

		headers := map[string]string{
			"Origin": "https://notallowed.com",
		}

		c, w := TestContext("GET", "/api/users", headers)

		var nextCalled bool
		next := func(c *gin.Context) {
			nextCalled = true
			c.Status(http.StatusOK)
		}

		middleware(next)(c)

		// 即使origin不匹配，实际请求也应该继续处理
		if !nextCalled {
			t.Error("Should still call next handler even with invalid origin")
		}

		// 但不应该设置CORS头部
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("Should not set Allow-Origin header for invalid origin")
		}
	})

	t.Run("Wildcard origin in actual request", func(t *testing.T) {
		middleware := CORSDefault() // Uses wildcard

		headers := map[string]string{
			"Origin": "https://example.com",
		}

		c, w := TestContext("GET", "/api/users", headers)

		middleware(func(c *gin.Context) {
			c.Status(http.StatusOK)
		})(c)

		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("Should set wildcard origin header")
		}

		// 通配符不应该设置Vary头
		if w.Header().Get("Vary") != "" {
			t.Error("Should not set Vary header for wildcard origin")
		}
	})
}

func TestCORSHeaderConfiguration(t *testing.T) {
	t.Run("Custom allow methods", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithAllowMethods("GET", "POST", "PUT"),
		)

		headers := map[string]string{
			"Origin":                        "https://example.com",
			"Access-Control-Request-Method": "POST",
		}

		c, w := TestContext("OPTIONS", "/api/users", headers)

		middleware(func(c *gin.Context) {})(c)

		allowMethods := w.Header().Get("Access-Control-Allow-Methods")
		expectedMethods := []string{"GET", "POST", "PUT"}

		for _, method := range expectedMethods {
			if !strings.Contains(allowMethods, method) {
				t.Errorf("Allow-Methods should contain %s, got: %s", method, allowMethods)
			}
		}
	})

	t.Run("Custom allow headers", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithAllowHeaders("Content-Type", "Authorization", "X-Custom"),
		)

		headers := map[string]string{
			"Origin":                         "https://example.com",
			"Access-Control-Request-Method":  "POST",
			"Access-Control-Request-Headers": "authorization, x-custom",
		}

		c, w := TestContext("OPTIONS", "/api/users", headers)

		middleware(func(c *gin.Context) {})(c)

		allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
		expectedHeaders := []string{"Content-Type", "Authorization", "X-Custom"}

		for _, header := range expectedHeaders {
			if !strings.Contains(allowHeaders, header) {
				t.Errorf("Allow-Headers should contain %s, got: %s", header, allowHeaders)
			}
		}
	})

	t.Run("Expose headers configuration", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithExposeHeaders("Content-Length", "X-Custom-Response"),
		)

		headers := map[string]string{
			"Origin": "https://example.com",
		}

		c, w := TestContext("GET", "/api/users", headers)

		middleware(func(c *gin.Context) {
			c.Status(http.StatusOK)
		})(c)

		exposeHeaders := w.Header().Get("Access-Control-Expose-Headers")
		expectedHeaders := []string{"Content-Length", "X-Custom-Response"}

		for _, header := range expectedHeaders {
			if !strings.Contains(exposeHeaders, header) {
				t.Errorf("Expose-Headers should contain %s, got: %s", header, exposeHeaders)
			}
		}
	})

	t.Run("Credentials configuration", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithAllowCredentials(true),
		)

		headers := map[string]string{
			"Origin": "https://example.com",
		}

		c, w := TestContext("GET", "/api/users", headers)

		middleware(func(c *gin.Context) {
			c.Status(http.StatusOK)
		})(c)

		if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Error("Should set Allow-Credentials header when enabled")
		}
	})

	t.Run("Max age configuration", func(t *testing.T) {
		middleware := CORS(
			WithAllowOrigins("https://example.com"),
			WithMaxAge(24*time.Hour),
		)

		headers := map[string]string{
			"Origin":                        "https://example.com",
			"Access-Control-Request-Method": "POST",
		}

		c, w := TestContext("OPTIONS", "/api/users", headers)

		middleware(func(c *gin.Context) {})(c)

		maxAge := w.Header().Get("Access-Control-Max-Age")
		expectedMaxAge := "86400" // 24 hours in seconds

		if maxAge != expectedMaxAge {
			t.Errorf("Expected Max-Age %s, got %s", expectedMaxAge, maxAge)
		}
	})
}

func TestCORSHelperFunctions(t *testing.T) {
	t.Run("isOriginAllowed", func(t *testing.T) {
		// 测试空列表
		if isOriginAllowed([]string{}, "https://example.com") {
			t.Error("Empty allowed origins should return false")
		}

		// 测试通配符
		if !isOriginAllowed([]string{"*"}, "https://example.com") {
			t.Error("Wildcard should allow any origin")
		}

		// 测试精确匹配
		if !isOriginAllowed([]string{"https://example.com"}, "https://example.com") {
			t.Error("Should allow exact origin match")
		}

		// 测试不匹配
		if isOriginAllowed([]string{"https://allowed.com"}, "https://notallowed.com") {
			t.Error("Should not allow non-matching origin")
		}

		// 测试多个源
		allowed := []string{"https://app1.com", "https://app2.com", "https://app3.com"}
		if !isOriginAllowed(allowed, "https://app2.com") {
			t.Error("Should find origin in multiple allowed origins")
		}
	})

	t.Run("areHeadersAllowed", func(t *testing.T) {
		allowed := []string{"Content-Type", "Authorization", "X-Custom"}

		// 测试单个允许的头部
		if !areHeadersAllowed(allowed, "Content-Type") {
			t.Error("Should allow single allowed header")
		}

		// 测试多个允许的头部
		if !areHeadersAllowed(allowed, "Content-Type, Authorization") {
			t.Error("Should allow multiple allowed headers")
		}

		// 测试不允许的头部
		if areHeadersAllowed(allowed, "X-Forbidden") {
			t.Error("Should not allow forbidden header")
		}

		// 测试混合（部分允许，部分不允许）
		if areHeadersAllowed(allowed, "Content-Type, X-Forbidden") {
			t.Error("Should not allow when any header is forbidden")
		}

		// 测试大小写不敏感
		if !areHeadersAllowed(allowed, "content-type, AUTHORIZATION") {
			t.Error("Header checking should be case insensitive")
		}

		// 测试带空格的头部
		if !areHeadersAllowed(allowed, " Content-Type , Authorization ") {
			t.Error("Should handle headers with spaces")
		}
	})

	t.Run("isHeaderAllowed", func(t *testing.T) {
		allowed := []string{"Content-Type", "Authorization"}

		// 测试精确匹配
		if !isHeaderAllowed(allowed, "Content-Type") {
			t.Error("Should allow exact header match")
		}

		// 测试大小写不敏感
		if !isHeaderAllowed(allowed, "content-type") {
			t.Error("Should be case insensitive")
		}

		if !isHeaderAllowed(allowed, "AUTHORIZATION") {
			t.Error("Should be case insensitive for uppercase")
		}

		// 测试不匹配
		if isHeaderAllowed(allowed, "X-Custom") {
			t.Error("Should not allow non-matching header")
		}
	})
}

func TestCORSEdgeCases(t *testing.T) {
	t.Run("Empty origin header", func(t *testing.T) {
		middleware := CORS(WithAllowOrigins("https://example.com"))

		c, w := TestContext("GET", "/api/users", nil) // No Origin header

		middleware(func(c *gin.Context) {
			c.Status(http.StatusOK)
		})(c)

		// 没有Origin头的请求不应该设置CORS头部
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("Should not set CORS headers when Origin is missing")
		}
	})

	t.Run("OPTIONS request without CORS headers", func(t *testing.T) {
		middleware := CORS(WithAllowOrigins("https://example.com"))

		c, w := TestContext("OPTIONS", "/api/users", nil) // No CORS headers

		middleware(func(c *gin.Context) {
			c.Status(http.StatusOK)
		})(c)

		// 没有Origin的OPTIONS请求应该被拒绝
		if w.Code != http.StatusForbidden {
			t.Error("OPTIONS request without Origin should be forbidden")
		}
	})

	t.Run("Multiple origins in allow list", func(t *testing.T) {
		origins := []string{
			"https://app1.example.com",
			"https://app2.example.com",
			"https://admin.example.com",
		}

		middleware := CORS(WithAllowOrigins(origins...))

		for _, origin := range origins {
			headers := map[string]string{"Origin": origin}
			c, w := TestContext("GET", "/test", headers)

			middleware(func(c *gin.Context) {
				c.Status(http.StatusOK)
			})(c)

			if w.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Errorf("Should allow origin %s", origin)
			}
		}
	})
}
