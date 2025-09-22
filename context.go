package ginx

import (
	"time"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// Context Keys - Typed Context Key Management
// ============================================================================

// contextKey defines a private type for context keys to avoid conflicts
type contextKey string

// Define all context keys as private constants
const (
	userIDKey         contextKey = "ginx.user_id"
	userRolesKey      contextKey = "ginx.user_roles"
	tokenIDKey        contextKey = "ginx.token_id"
	tokenExpiresAtKey contextKey = "ginx.token_expires_at"
	tokenIssuedAtKey  contextKey = "ginx.token_issued_at"
)

// ============================================================================
// User Context Helpers
// ============================================================================

// SetUserID sets the user ID in the context
func SetUserID(c *gin.Context, userID string) {
	c.Set(string(userIDKey), userID)
}

// GetUserID gets the user ID from the context
func GetUserID(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(userIDKey))
	if !exists {
		return "", false
	}
	if id, ok := value.(string); ok {
		return id, true
	}
	return "", false
}

// SetUserRoles sets the user roles in the context
func SetUserRoles(c *gin.Context, roles []string) {
	c.Set(string(userRolesKey), roles)
}

// GetUserRoles gets the user roles from the context
func GetUserRoles(c *gin.Context) ([]string, bool) {
	value, exists := c.Get(string(userRolesKey))
	if !exists {
		return nil, false
	}
	if roles, ok := value.([]string); ok {
		return roles, true
	}
	return nil, false
}

// ============================================================================
// Token Context Helpers
// ============================================================================

// SetTokenID sets the token ID in the context
func SetTokenID(c *gin.Context, tokenID string) {
	c.Set(string(tokenIDKey), tokenID)
}

// GetTokenID gets the token ID from the context
func GetTokenID(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(tokenIDKey))
	if !exists {
		return "", false
	}
	if id, ok := value.(string); ok {
		return id, true
	}
	return "", false
}

// SetTokenExpiresAt sets the token expiration time in the context
func SetTokenExpiresAt(c *gin.Context, expiresAt time.Time) {
	c.Set(string(tokenExpiresAtKey), expiresAt)
}

// GetTokenExpiresAt gets the token expiration time from the context
func GetTokenExpiresAt(c *gin.Context) (time.Time, bool) {
	value, exists := c.Get(string(tokenExpiresAtKey))
	if !exists {
		return time.Time{}, false
	}
	if t, ok := value.(time.Time); ok {
		return t, true
	}
	return time.Time{}, false
}

// SetTokenIssuedAt sets the token issued time in the context
func SetTokenIssuedAt(c *gin.Context, issuedAt time.Time) {
	c.Set(string(tokenIssuedAtKey), issuedAt)
}

// GetTokenIssuedAt gets the token issued time from the context
func GetTokenIssuedAt(c *gin.Context) (time.Time, bool) {
	value, exists := c.Get(string(tokenIssuedAtKey))
	if !exists {
		return time.Time{}, false
	}
	if t, ok := value.(time.Time); ok {
		return t, true
	}
	return time.Time{}, false
}

// ============================================================================
// Convenience Functions
// ============================================================================

// GetUserIDOrAbort gets user ID from context or aborts with 401
func GetUserIDOrAbort(c *gin.Context) (string, bool) {
	userID, exists := GetUserID(c)
	if !exists {
		c.AbortWithStatusJSON(401, gin.H{"error": "user not authenticated"})
		return "", false
	}
	return userID, true
}
