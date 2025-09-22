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
			SetUserID(c, parsedToken.UserID)
			SetUserRoles(c, parsedToken.Roles)
			SetTokenID(c, parsedToken.TokenID)
			SetTokenExpiresAt(c, parsedToken.ExpiresAt)
			SetTokenIssuedAt(c, parsedToken.IssuedAt)

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
