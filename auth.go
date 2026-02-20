package ginx

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/jwt"
)

// AuthConfig configures JWT auth token extraction behavior.
type AuthConfig struct {
	AllowQueryToken bool `json:"allow_query_token"`
}

func defaultAuthConfig() *AuthConfig {
	return &AuthConfig{
		AllowQueryToken: false,
	}
}

// WithAuthQueryToken controls whether query token fallback is allowed.
// Disabled by default for credential leakage prevention.
func WithAuthQueryToken(allow bool) Option[AuthConfig] {
	return func(c *AuthConfig) {
		c.AllowQueryToken = allow
	}
}

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
func Auth(jwtService jwt.Service, options ...Option[AuthConfig]) Middleware {
	if jwtService == nil {
		panic("auth middleware requires non-nil jwt service")
	}

	config := defaultAuthConfig()
	for _, option := range options {
		option(config)
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			// Get token from Authorization header (or query parameter when explicitly enabled)
			tokenString := extractTokenWithConfig(c, config.AllowQueryToken)
			if tokenString == "" {
				AbortWithError(c, 401, "missing token")
				return
			}

			// Validate and parse the token
			parsedToken, err := jwtService.ValidateAndParse(tokenString)
			if err != nil {
				AbortWithError(c, 401, "invalid token")
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

// extractToken extracts the JWT token from the Authorization header.
// Query parameter fallback is disabled by default.
func extractToken(c *gin.Context) string {
	return extractTokenWithConfig(c, false)
}

// extractTokenWithConfig extracts token with configurable query fallback behavior.
// When the Authorization header is present but not in Bearer format (e.g., "Basic ..."),
// this intentionally returns an empty string. The calling middleware then responds with
// "missing token" (401), which is the correct behavior — the endpoint requires Bearer
// auth, so a non-Bearer scheme is equivalent to no valid token being provided.
func extractTokenWithConfig(c *gin.Context, allowQueryToken bool) string {
	header := c.GetHeader("Authorization")
	if header != "" {
		parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token := strings.TrimSpace(parts[1])
			if token != "" {
				return token
			}
		}
	}

	if allowQueryToken {
		if token := c.Query("token"); token != "" {
			return token
		}
	}

	return ""
}
