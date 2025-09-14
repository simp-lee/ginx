package ginx

import (
	"github.com/gin-gonic/gin"
	"github.com/simp-lee/rbac"
)

// ============================================================================
// Middleware - RBAC Authorization
// ============================================================================

// getUserIDOrAbort get user ID from context or abort with 401
func getUserIDOrAbort(c *gin.Context) (string, bool) {
	userID, exists := getUserID(c)
	if !exists {
		c.AbortWithStatusJSON(401, gin.H{"error": "user not authenticated"})
		return "", false
	}
	return userID, true
}

// RequirePermission based on roles and direct user permission checking middleware
func RequirePermission(service rbac.Service, resource, action string) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			userID, ok := getUserIDOrAbort(c)
			if !ok {
				return
			}

			hasPermission, err := service.HasPermission(userID, resource, action)
			if err != nil {
				c.AbortWithStatusJSON(500, gin.H{"error": "permission check failed"})
				return
			}

			if !hasPermission {
				c.AbortWithStatusJSON(403, gin.H{"error": "permission denied"})
				return
			}

			next(c)
		}
	}
}

// RequireRolePermission based on role based permission only checking middleware
func RequireRolePermission(service rbac.Service, resource, action string) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			userID, ok := getUserIDOrAbort(c)
			if !ok {
				return
			}

			hasPermission, err := service.HasRolePermission(userID, resource, action)
			if err != nil {
				c.AbortWithStatusJSON(500, gin.H{"error": "permission check failed"})
				return
			}

			if !hasPermission {
				c.AbortWithStatusJSON(403, gin.H{"error": "insufficient role permissions"})
				return
			}

			next(c)
		}
	}
}

// RequireUserPermission based on direct user permission only checking middleware
func RequireUserPermission(service rbac.Service, resource, action string) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			userID, ok := getUserIDOrAbort(c)
			if !ok {
				return
			}

			hasPermission, err := service.HasUserPermission(userID, resource, action)
			if err != nil {
				c.AbortWithStatusJSON(500, gin.H{"error": "permission check failed"})
				return
			}

			if !hasPermission {
				c.AbortWithStatusJSON(403, gin.H{"error": "insufficient user permissions"})
				return
			}

			next(c)
		}
	}
}

// ============================================================================
// Conditions - RBAC Authorization
// ============================================================================

// IsAuthenticated checks if the user is authenticated
func IsAuthenticated() Condition {
	return func(c *gin.Context) bool {
		_, exists := getUserID(c)
		return exists
	}
}

// HasPermission checks combined role and direct user permissions
func HasPermission(service rbac.Service, resource, action string) Condition {
	return func(c *gin.Context) bool {
		userID, exists := getUserID(c)
		if !exists {
			return false
		}
		hasPermission, err := service.HasPermission(userID, resource, action)
		return err == nil && hasPermission
	}
}

// HasRolePermission checks role based permissions only
func HasRolePermission(service rbac.Service, resource, action string) Condition {
	return func(c *gin.Context) bool {
		userID, exists := getUserID(c)
		if !exists {
			return false
		}
		hasPermission, err := service.HasRolePermission(userID, resource, action)
		return err == nil && hasPermission
	}
}

// HasUserPermission checks direct user permissions only
func HasUserPermission(service rbac.Service, resource, action string) Condition {
	return func(c *gin.Context) bool {
		userID, exists := getUserID(c)
		if !exists {
			return false
		}
		hasPermission, err := service.HasUserPermission(userID, resource, action)
		return err == nil && hasPermission
	}
}
