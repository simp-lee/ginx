package ginx

import (
	"github.com/gin-gonic/gin"
	"github.com/simp-lee/rbac"
)

// ============================================================================
// Middleware - RBAC Authorization
// ============================================================================

// permissionCheckFunc is the function signature for checking permissions via the rbac.Service.
type permissionCheckFunc func(service rbac.Service, userID, resource, action string) (bool, error)

// requirePermission is the internal helper that all permission middleware delegates to.
// It validates the service, extracts the user ID, calls checkFn, and aborts with the
// appropriate status and denyMsg on failure.
func requirePermission(service rbac.Service, resource, action, denyMsg string, checkFn permissionCheckFunc) Middleware {
	if service == nil {
		panic("rbac middleware requires non-nil service")
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			userID, ok := GetUserIDOrAbort(c)
			if !ok {
				return
			}

			hasPermission, err := checkFn(service, userID, resource, action)
			if err != nil {
				c.AbortWithStatusJSON(500, gin.H{"error": "permission check failed"})
				return
			}

			if !hasPermission {
				c.AbortWithStatusJSON(403, gin.H{"error": denyMsg})
				return
			}

			next(c)
		}
	}
}

// RequirePermission based on roles and direct user permission checking middleware
func RequirePermission(service rbac.Service, resource, action string) Middleware {
	return requirePermission(service, resource, action, "permission denied",
		func(s rbac.Service, userID, res, act string) (bool, error) {
			return s.HasPermission(userID, res, act)
		})
}

// RequireRolePermission based on role based permission only checking middleware
func RequireRolePermission(service rbac.Service, resource, action string) Middleware {
	return requirePermission(service, resource, action, "insufficient role permissions",
		func(s rbac.Service, userID, res, act string) (bool, error) {
			return s.HasRolePermission(userID, res, act)
		})
}

// RequireUserPermission based on direct user permission only checking middleware
func RequireUserPermission(service rbac.Service, resource, action string) Middleware {
	return requirePermission(service, resource, action, "insufficient user permissions",
		func(s rbac.Service, userID, res, act string) (bool, error) {
			return s.HasUserPermission(userID, res, act)
		})
}

// ============================================================================
// Conditions - RBAC Authorization
// ============================================================================

// IsAuthenticated checks if the user is authenticated
func IsAuthenticated() Condition {
	return func(c *gin.Context) bool {
		_, exists := GetUserID(c)
		return exists
	}
}

// HasPermission checks combined role and direct user permissions
func HasPermission(service rbac.Service, resource, action string) Condition {
	if service == nil {
		panic("rbac condition requires non-nil service")
	}

	return func(c *gin.Context) bool {
		userID, exists := GetUserID(c)
		if !exists {
			return false
		}
		hasPermission, err := service.HasPermission(userID, resource, action)
		return err == nil && hasPermission
	}
}

// HasRolePermission checks role based permissions only
func HasRolePermission(service rbac.Service, resource, action string) Condition {
	if service == nil {
		panic("rbac condition requires non-nil service")
	}

	return func(c *gin.Context) bool {
		userID, exists := GetUserID(c)
		if !exists {
			return false
		}
		hasPermission, err := service.HasRolePermission(userID, resource, action)
		return err == nil && hasPermission
	}
}

// HasUserPermission checks direct user permissions only
func HasUserPermission(service rbac.Service, resource, action string) Condition {
	if service == nil {
		panic("rbac condition requires non-nil service")
	}

	return func(c *gin.Context) bool {
		userID, exists := GetUserID(c)
		if !exists {
			return false
		}
		hasPermission, err := service.HasUserPermission(userID, resource, action)
		return err == nil && hasPermission
	}
}
