package ginx

import (
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// Logical combinators for conditions

// And all conditions must be true
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

// Or at least one condition is true
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

// Not all conditions must be false
func Not(cond Condition) Condition {
	return func(c *gin.Context) bool {
		return !cond(c)
	}
}

// Path conditions

// PathIs exact path match
func PathIs(paths ...string) Condition {
	return func(c *gin.Context) bool {
		currentPath := c.Request.URL.Path
		return slices.Contains(paths, currentPath)
	}
}

// PathHasPrefix checks if the path has the specified prefix
func PathHasPrefix(prefix string) Condition {
	return func(c *gin.Context) bool {
		return strings.HasPrefix(c.Request.URL.Path, prefix)
	}
}

// PathHasSuffix checks if the path has the specified suffix
func PathHasSuffix(suffix string) Condition {
	return func(c *gin.Context) bool {
		return strings.HasSuffix(c.Request.URL.Path, suffix)
	}
}

// PathMatches checks if the path matches the specified regex pattern
func PathMatches(pattern string) Condition {
	re := regexp.MustCompile(pattern)
	return func(c *gin.Context) bool {
		return re.MatchString(c.Request.URL.Path)
	}
}

// HTTP conditions

// MethodIs checks if the HTTP method matches any of the specified methods
func MethodIs(methods ...string) Condition {
	return func(c *gin.Context) bool {
		currentMethod := c.Request.Method
		return slices.Contains(methods, currentMethod)
	}
}

// HeaderExists checks if the request header exists
func HeaderExists(key string) Condition {
	return func(c *gin.Context) bool {
		value := c.GetHeader(key)
		return value != ""
	}
}

// HeaderEquals checks if the request header value matches
func HeaderEquals(key, value string) Condition {
	return func(c *gin.Context) bool {
		return c.GetHeader(key) == value
	}
}

// ContentTypeIs checks if the Content-Type matches any of the specified types
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

// Custom conditions

// Custom creates a custom condition
func Custom(fn func(*gin.Context) bool) Condition {
	return fn
}
