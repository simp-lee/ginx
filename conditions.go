package ginx

import (
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// 逻辑组合函数

// And 逻辑与，所有条件都必须为真
func And(conds ...Condition) Condition {
	return func(c *gin.Context) bool {
		for _, cond := range conds {
			if !cond(c) {
				return false
			}
		}
		return true
	}
}

// Or 逻辑或，任一条件为真即可
func Or(conds ...Condition) Condition {
	return func(c *gin.Context) bool {
		for _, cond := range conds {
			if cond(c) {
				return true
			}
		}
		return false
	}
}

// Not 逻辑非，条件取反
func Not(cond Condition) Condition {
	return func(c *gin.Context) bool {
		return !cond(c)
	}
}

// 路径条件函数

// PathIs 路径精确匹配
func PathIs(paths ...string) Condition {
	return func(c *gin.Context) bool {
		currentPath := c.Request.URL.Path
		return slices.Contains(paths, currentPath)
	}
}

// PathHasPrefix 路径前缀匹配
func PathHasPrefix(prefix string) Condition {
	return func(c *gin.Context) bool {
		return strings.HasPrefix(c.Request.URL.Path, prefix)
	}
}

// PathHasSuffix 路径后缀匹配
func PathHasSuffix(suffix string) Condition {
	return func(c *gin.Context) bool {
		return strings.HasSuffix(c.Request.URL.Path, suffix)
	}
}

// PathMatches 路径正则匹配
func PathMatches(pattern string) Condition {
	re := regexp.MustCompile(pattern)
	return func(c *gin.Context) bool {
		return re.MatchString(c.Request.URL.Path)
	}
}

// HTTP 条件函数

// MethodIs HTTP 方法匹配
func MethodIs(methods ...string) Condition {
	return func(c *gin.Context) bool {
		currentMethod := c.Request.Method
		return slices.Contains(methods, currentMethod)
	}
}

// HeaderExists 检查请求头是否存在
func HeaderExists(key string) Condition {
	return func(c *gin.Context) bool {
		value := c.GetHeader(key)
		return value != ""
	}
}

// HeaderEquals 检查请求头值是否匹配
func HeaderEquals(key, value string) Condition {
	return func(c *gin.Context) bool {
		return c.GetHeader(key) == value
	}
}

// ContentTypeIs 检查 Content-Type 是否匹配
func ContentTypeIs(contentTypes ...string) Condition {
	return func(c *gin.Context) bool {
		currentContentType := c.GetHeader("Content-Type")
		for _, contentType := range contentTypes {
			if strings.Contains(currentContentType, contentType) {
				return true
			}
		}
		return false
	}
}

// 自定义条件函数

// Custom 自定义条件函数
func Custom(fn func(*gin.Context) bool) Condition {
	return fn
}
