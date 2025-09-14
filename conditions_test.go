package ginx

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLogicConditions(t *testing.T) {
	t.Run("And - all conditions true", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/users", nil)

		cond := And(
			PathHasPrefix("/api"),
			MethodIs("GET"),
		)

		if !cond(c) {
			t.Error("And condition should return true when all conditions are true")
		}
	})

	t.Run("And - one condition false", func(t *testing.T) {
		c, _ := TestContext("POST", "/api/users", nil)

		cond := And(
			PathHasPrefix("/api"),
			MethodIs("GET"), // This is false
		)

		if cond(c) {
			t.Error("And condition should return false when any condition is false")
		}
	})

	t.Run("Or - one condition true", func(t *testing.T) {
		c, _ := TestContext("POST", "/api/users", nil)

		cond := Or(
			MethodIs("GET"),  // This is false
			MethodIs("POST"), // This is true
		)

		if !cond(c) {
			t.Error("Or condition should return true when any condition is true")
		}
	})

	t.Run("Or - all conditions false", func(t *testing.T) {
		c, _ := TestContext("DELETE", "/api/users", nil)

		cond := Or(
			MethodIs("GET"),
			MethodIs("POST"),
		)

		if cond(c) {
			t.Error("Or condition should return false when all conditions are false")
		}
	})

	t.Run("Not - reverse condition", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/users", nil)

		cond := Not(MethodIs("POST"))

		if !cond(c) {
			t.Error("Not condition should return true when inner condition is false")
		}
	})
}

func TestPathConditions(t *testing.T) {
	t.Run("PathIs - exact match", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/users", nil)

		cond := PathIs("/api/users", "/api/posts")

		if !cond(c) {
			t.Error("PathIs should return true for exact path match")
		}
	})

	t.Run("PathIs - no match", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/orders", nil)

		cond := PathIs("/api/users", "/api/posts")

		if cond(c) {
			t.Error("PathIs should return false for non-matching path")
		}
	})

	t.Run("PathHasPrefix - match", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/v1/users", nil)

		cond := PathHasPrefix("/api")

		if !cond(c) {
			t.Error("PathHasPrefix should return true for matching prefix")
		}
	})

	t.Run("PathHasPrefix - no match", func(t *testing.T) {
		c, _ := TestContext("GET", "/web/dashboard", nil)

		cond := PathHasPrefix("/api")

		if cond(c) {
			t.Error("PathHasPrefix should return false for non-matching prefix")
		}
	})

	t.Run("PathHasSuffix - match", func(t *testing.T) {
		c, _ := TestContext("GET", "/files/document.pdf", nil)

		cond := PathHasSuffix(".pdf")

		if !cond(c) {
			t.Error("PathHasSuffix should return true for matching suffix")
		}
	})

	t.Run("PathHasSuffix - no match", func(t *testing.T) {
		c, _ := TestContext("GET", "/files/document.txt", nil)

		cond := PathHasSuffix(".pdf")

		if cond(c) {
			t.Error("PathHasSuffix should return false for non-matching suffix")
		}
	})

	t.Run("PathMatches - regex match", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/users/123", nil)

		cond := PathMatches(`^/api/users/\d+$`)

		if !cond(c) {
			t.Error("PathMatches should return true for matching regex pattern")
		}
	})

	t.Run("PathMatches - regex no match", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/users/abc", nil)

		cond := PathMatches(`^/api/users/\d+$`)

		if cond(c) {
			t.Error("PathMatches should return false for non-matching regex pattern")
		}
	})
}

func TestHTTPConditions(t *testing.T) {
	t.Run("MethodIs - single method match", func(t *testing.T) {
		c, _ := TestContext("POST", "/api/users", nil)

		cond := MethodIs("POST")

		if !cond(c) {
			t.Error("MethodIs should return true for matching method")
		}
	})

	t.Run("MethodIs - multiple methods", func(t *testing.T) {
		c, _ := TestContext("PUT", "/api/users", nil)

		cond := MethodIs("POST", "PUT", "PATCH")

		if !cond(c) {
			t.Error("MethodIs should return true when method matches any in the list")
		}
	})

	t.Run("MethodIs - no match", func(t *testing.T) {
		c, _ := TestContext("DELETE", "/api/users", nil)

		cond := MethodIs("GET", "POST")

		if cond(c) {
			t.Error("MethodIs should return false for non-matching method")
		}
	})

	t.Run("HeaderExists - header present", func(t *testing.T) {
		headers := map[string]string{
			"Authorization": "Bearer token123",
		}
		c, _ := TestContext("GET", "/api/users", headers)

		cond := HeaderExists("Authorization")

		if !cond(c) {
			t.Error("HeaderExists should return true when header exists")
		}
	})

	t.Run("HeaderExists - header missing", func(t *testing.T) {
		c, _ := TestContext("GET", "/api/users", nil)

		cond := HeaderExists("Authorization")

		if cond(c) {
			t.Error("HeaderExists should return false when header is missing")
		}
	})

	t.Run("HeaderEquals - exact match", func(t *testing.T) {
		headers := map[string]string{
			"X-API-Version": "v1",
		}
		c, _ := TestContext("GET", "/api/users", headers)

		cond := HeaderEquals("X-API-Version", "v1")

		if !cond(c) {
			t.Error("HeaderEquals should return true for exact header value match")
		}
	})

	t.Run("HeaderEquals - no match", func(t *testing.T) {
		headers := map[string]string{
			"X-API-Version": "v2",
		}
		c, _ := TestContext("GET", "/api/users", headers)

		cond := HeaderEquals("X-API-Version", "v1")

		if cond(c) {
			t.Error("HeaderEquals should return false for non-matching header value")
		}
	})

	t.Run("ContentTypeIs - exact match", func(t *testing.T) {
		headers := map[string]string{
			"Content-Type": "application/json",
		}
		c, _ := TestContext("POST", "/api/users", headers)

		cond := ContentTypeIs("application/json")

		if !cond(c) {
			t.Error("ContentTypeIs should return true for matching content type")
		}
	})

	t.Run("ContentTypeIs - partial match", func(t *testing.T) {
		headers := map[string]string{
			"Content-Type": "application/json; charset=utf-8",
		}
		c, _ := TestContext("POST", "/api/users", headers)

		cond := ContentTypeIs("application/json")

		if !cond(c) {
			t.Error("ContentTypeIs should return true for partial content type match")
		}
	})

	t.Run("ContentTypeIs - multiple types", func(t *testing.T) {
		headers := map[string]string{
			"Content-Type": "text/plain",
		}
		c, _ := TestContext("POST", "/api/users", headers)

		cond := ContentTypeIs("application/json", "text/plain", "text/html")

		if !cond(c) {
			t.Error("ContentTypeIs should return true when content type matches any in the list")
		}
	})

	t.Run("ContentTypeIs - no match", func(t *testing.T) {
		headers := map[string]string{
			"Content-Type": "application/xml",
		}
		c, _ := TestContext("POST", "/api/users", headers)

		cond := ContentTypeIs("application/json", "text/plain")

		if cond(c) {
			t.Error("ContentTypeIs should return false for non-matching content type")
		}
	})
}

func TestCustomCondition(t *testing.T) {
	t.Run("Custom - user defined condition", func(t *testing.T) {
		headers := map[string]string{
			"User-Agent": "TestBot/1.0",
		}
		c, _ := TestContext("GET", "/api/health", headers)

		// 自定义条件：检查是否为测试机器人
		isTestBot := Custom(func(ctx *gin.Context) bool {
			userAgent := ctx.GetHeader("User-Agent")
			return userAgent == "TestBot/1.0"
		})

		if !isTestBot(c) {
			t.Error("Custom condition should return true for test bot user agent")
		}
	})

	t.Run("Custom - complex condition", func(t *testing.T) {
		c, _ := TestContext("POST", "/api/admin/users", nil)

		// 自定义条件：管理员 API 且为 POST 请求
		isAdminPost := Custom(func(ctx *gin.Context) bool {
			return ctx.Request.Method == "POST" &&
				strings.HasPrefix(ctx.Request.URL.Path, "/api/admin/")
		})

		if !isAdminPost(c) {
			t.Error("Custom condition should handle complex logic correctly")
		}
	})
}

func TestComplexConditionCombinations(t *testing.T) {
	t.Run("Complex combination", func(t *testing.T) {
		headers := map[string]string{
			"Authorization": "Bearer token123",
			"Content-Type":  "application/json",
		}
		c, _ := TestContext("POST", "/api/admin/users", headers)

		// 复杂条件：(管理员路径 AND POST方法) AND (有认证 OR JSON内容)
		complexCond := And(
			And(
				PathHasPrefix("/api/admin"),
				MethodIs("POST"),
			),
			Or(
				HeaderExists("Authorization"),
				ContentTypeIs("application/json"),
			),
		)

		if !complexCond(c) {
			t.Error("Complex condition combination should work correctly")
		}
	})

	t.Run("Complex combination with negation", func(t *testing.T) {
		c, _ := TestContext("GET", "/public/health", nil)

		// 条件：不是管理员路径 AND 不需要认证
		publicCond := And(
			Not(PathHasPrefix("/api/admin")),
			Not(HeaderExists("Authorization")),
		)

		if !publicCond(c) {
			t.Error("Complex condition with negation should work correctly")
		}
	})
}
