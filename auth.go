package ginx

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/jwt"
)

// ============================================================================
// Middleware - JWT Authentication
// ============================================================================

// jwtService, _ := jwt.New(secret, opts...)

// Service interface from the JWT library
// type Service interface {
//     GenerateToken(userID string, roles []string, expiresIn time.Duration) (string, error)
//     ValidateToken(tokenString string) (*Token, error)
//     ValidateAndParse(tokenString string) (*Token, error)          // Convenience method
//     RefreshToken(tokenString string) (string, error)              // Preserves original duration
//     RefreshTokenExtend(tokenString string, extendsIn time.Duration) (string, error) // Extends with new duration
//     RevokeToken(tokenString string) error
//     IsTokenRevoked(tokenID string) bool
//     ParseToken(tokenString string) (*Token, error)
//     RevokeAllUserTokens(userID string) error
//     Close()
// }

// Auth is JWT authentication middleware.
func Auth(jwtService jwt.Service) Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			// Get token from Authorization header or query parameter
			tokenString := extractToken(c)
			if tokenString == "" {
				c.AbortWithStatusJSON(401, gin.H{"error": "missing token"})
				return
			}

			// Validate and parse the token
			parsedToken, err := jwtService.ValidateAndParse(tokenString)
			if err != nil {
				c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
				return
			}

			// Set user information to context
			c.Set("user_id", parsedToken.UserID)
			c.Set("user_roles", parsedToken.Roles)
			c.Set("token_id", parsedToken.TokenID)
			c.Set("token_expires_at", parsedToken.ExpiresAt)
			c.Set("token_issued_at", parsedToken.IssuedAt)

			next(c)
		}
	}
}

// extractToken extracts the JWT token from the Authorization header or query parameter.
func extractToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if header != "" && strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}

	// Fallback to query parameter
	if token := c.Query("token"); token != "" {
		return token
	}

	return ""
}

// getUserID extracts the user ID from the context.
func getUserID(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return "", false
	}
	if id, ok := userID.(string); ok {
		return id, true
	}
	return "", false
}
