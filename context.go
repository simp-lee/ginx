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
	requestIDKey      contextKey = "ginx.request_id"
	errorFormatterKey contextKey = "ginx.error_formatter"
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
// Request Context Helpers
// ============================================================================

// SetRequestID sets the request ID in the context
func SetRequestID(c *gin.Context, id string) {
	c.Set(string(requestIDKey), id)
}

// GetRequestID gets the request ID from the context
func GetRequestID(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(requestIDKey))
	if !exists {
		return "", false
	}
	if id, ok := value.(string); ok {
		return id, true
	}
	return "", false
}

// ============================================================================
// Convenience Functions
// ============================================================================

// GetUserIDOrAbort gets user ID from context or aborts with 401
func GetUserIDOrAbort(c *gin.Context) (string, bool) {
	userID, exists := GetUserID(c)
	if !exists {
		AbortWithError(c, 401, "user not authenticated")
		return "", false
	}
	return userID, true
}

// ============================================================================
// Error Formatting Helpers
// ============================================================================

// SetErrorFormatter sets the ErrorFormatter in the context.
func SetErrorFormatter(c *gin.Context, f ErrorFormatter) {
	c.Set(string(errorFormatterKey), f)
}

// GetErrorFormatter gets the ErrorFormatter from the context. Returns nil if not set.
func GetErrorFormatter(c *gin.Context) ErrorFormatter {
	value, exists := c.Get(string(errorFormatterKey))
	if !exists {
		return nil
	}
	if f, ok := value.(ErrorFormatter); ok {
		return f
	}
	return nil
}

// AbortWithError writes an error response using the ErrorFormatter if set,
// otherwise falls back to gin.H{"error": message}.
//
// Note: Unlike gin.Context.AbortWithError which takes an error value and
// appends to ctx.Errors without writing a JSON body, this function writes
// a JSON response immediately and aborts the chain.
func AbortWithError(c *gin.Context, statusCode int, message string) {
	if f := GetErrorFormatter(c); f != nil {
		c.AbortWithStatusJSON(statusCode, f(statusCode, message))
	} else {
		c.AbortWithStatusJSON(statusCode, gin.H{"error": message})
	}
}
