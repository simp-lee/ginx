package ginx

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock JWT Service for testing
type MockJWTService struct {
	mock.Mock
}

func (m *MockJWTService) GenerateToken(userID string, roles []string, expiresIn time.Duration) (string, error) {
	args := m.Called(userID, roles, expiresIn)
	return args.String(0), args.Error(1)
}

func (m *MockJWTService) ValidateToken(tokenString string) (*jwt.Token, error) {
	args := m.Called(tokenString)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*jwt.Token), args.Error(1)
}

func (m *MockJWTService) ValidateAndParse(tokenString string) (*jwt.Token, error) {
	args := m.Called(tokenString)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*jwt.Token), args.Error(1)
}

func (m *MockJWTService) RefreshToken(tokenString string) (string, error) {
	args := m.Called(tokenString)
	return args.String(0), args.Error(1)
}

func (m *MockJWTService) RefreshTokenExtend(tokenString string, extendsIn time.Duration) (string, error) {
	args := m.Called(tokenString, extendsIn)
	return args.String(0), args.Error(1)
}

func (m *MockJWTService) RevokeToken(tokenString string) error {
	args := m.Called(tokenString)
	return args.Error(0)
}

func (m *MockJWTService) IsTokenRevoked(tokenID string) bool {
	args := m.Called(tokenID)
	return args.Bool(0)
}

func (m *MockJWTService) ParseToken(tokenString string) (*jwt.Token, error) {
	args := m.Called(tokenString)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*jwt.Token), args.Error(1)
}

func (m *MockJWTService) RevokeAllUserTokens(userID string) error {
	args := m.Called(userID)
	return args.Error(0)
}

func (m *MockJWTService) Close() {
	m.Called()
}

func TestAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return 401 when no token provided", func(t *testing.T) {
		mockJWT := new(MockJWTService)
		c, w := TestContext("GET", "/test", nil)

		// Test with no Authorization header and no token query param
		authMiddleware := Auth(mockJWT)
		handler := authMiddleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "missing token", response["error"])

		mockJWT.AssertExpectations(t)
	})

	t.Run("should return 401 when token is invalid", func(t *testing.T) {
		mockJWT := new(MockJWTService)
		c, w := TestContext("GET", "/test", map[string]string{
			"Authorization": "Bearer invalid-token",
		})

		mockJWT.On("ValidateAndParse", "invalid-token").Return(nil, errors.New("invalid token"))

		authMiddleware := Auth(mockJWT)
		handler := authMiddleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "invalid token", response["error"])

		mockJWT.AssertExpectations(t)
	})

	t.Run("should set user context when token is valid", func(t *testing.T) {
		mockJWT := new(MockJWTService)
		c, w := TestContext("GET", "/test", map[string]string{
			"Authorization": "Bearer valid-token",
		})

		// Create a mock token
		mockToken := &jwt.Token{
			UserID:    "user123",
			Roles:     []string{"admin", "user"},
			TokenID:   "token123",
			ExpiresAt: time.Now().Add(time.Hour),
			IssuedAt:  time.Now(),
		}

		mockJWT.On("ValidateAndParse", "valid-token").Return(mockToken, nil)

		var contextUserID interface{}
		var contextRoles interface{}
		var contextTokenID interface{}

		authMiddleware := Auth(mockJWT)
		handler := authMiddleware(func(c *gin.Context) {
			contextUserID, _ = GetUserID(c)
			contextRoles, _ = GetUserRoles(c)
			contextTokenID, _ = GetTokenID(c)
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "user123", contextUserID)
		assert.Equal(t, []string{"admin", "user"}, contextRoles)
		assert.Equal(t, "token123", contextTokenID)

		mockJWT.AssertExpectations(t)
	})

	t.Run("should extract token from query parameter when header is missing", func(t *testing.T) {
		mockJWT := new(MockJWTService)
		c, w := TestContext("GET", "/test?token=query-token", nil)

		mockToken := &jwt.Token{
			UserID:  "user123",
			Roles:   []string{"user"},
			TokenID: "token123",
		}

		mockJWT.On("ValidateAndParse", "query-token").Return(mockToken, nil)

		authMiddleware := Auth(mockJWT)
		handler := authMiddleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockJWT.AssertExpectations(t)
	})

	t.Run("should prefer Authorization header over query parameter", func(t *testing.T) {
		mockJWT := new(MockJWTService)
		c, w := TestContext("GET", "/test?token=query-token", map[string]string{
			"Authorization": "Bearer header-token",
		})

		mockToken := &jwt.Token{
			UserID:  "user123",
			Roles:   []string{"user"},
			TokenID: "token123",
		}

		// Should call with header token, not query token
		mockJWT.On("ValidateAndParse", "header-token").Return(mockToken, nil)

		authMiddleware := Auth(mockJWT)
		handler := authMiddleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockJWT.AssertExpectations(t)
	})
}

func TestExtractToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should extract token from Bearer Authorization header", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", map[string]string{
			"Authorization": "Bearer test-token-123",
		})

		token := extractToken(c)
		assert.Equal(t, "test-token-123", token)
	})

	t.Run("should return empty string for non-Bearer Authorization header", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", map[string]string{
			"Authorization": "Basic dGVzdA==",
		})

		token := extractToken(c)
		assert.Equal(t, "", token)
	})

	t.Run("should extract token from query parameter when header is missing", func(t *testing.T) {
		c, _ := TestContext("GET", "/test?token=query-token-456", nil)

		token := extractToken(c)
		assert.Equal(t, "query-token-456", token)
	})

	t.Run("should return empty string when no token found", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)

		token := extractToken(c)
		assert.Equal(t, "", token)
	})

	t.Run("should prefer Authorization header over query parameter", func(t *testing.T) {
		c, _ := TestContext("GET", "/test?token=query-token", map[string]string{
			"Authorization": "Bearer header-token",
		})

		token := extractToken(c)
		assert.Equal(t, "header-token", token)
	})
}

func TestGetUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return user ID when exists in context", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		userID, exists := GetUserID(c)
		assert.True(t, exists)
		assert.Equal(t, "user123", userID)
	})

	t.Run("should return false when user_id not set in context", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)

		userID, exists := GetUserID(c)
		assert.False(t, exists)
		assert.Equal(t, "", userID)
	})

	t.Run("should return false when user_id is not a string", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)
		c.Set("user_id", 12345) // Not a string

		userID, exists := GetUserID(c)
		assert.False(t, exists)
		assert.Equal(t, "", userID)
	})

	t.Run("should handle nil user_id value", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)
		c.Set("user_id", nil)

		userID, exists := GetUserID(c)
		assert.False(t, exists)
		assert.Equal(t, "", userID)
	})
}

// Integration test with real JWT service
func TestAuthMiddlewareIntegration(t *testing.T) {
	t.Skip("Skipping integration test - requires real JWT service")

	// This test would use a real JWT service for integration testing
	// Uncomment and modify when testing with actual JWT service

	/*
		jwtService, err := jwt.New("test-secret")
		if err != nil {
			t.Fatal("Failed to create JWT service:", err)
		}
		defer jwtService.Close()

		// Generate a real token
		token, err := jwtService.GenerateToken("user123", []string{"user"}, time.Hour)
		if err != nil {
			t.Fatal("Failed to generate token:", err)
		}

		c, w := TestContext("GET", "/test", map[string]string{
			"Authorization": "Bearer " + token,
		})

		authMiddleware := Auth(jwtService)
		handler := authMiddleware(func(c *gin.Context) {
			userID, exists := c.Get("user_id")
			if !exists || userID != "user123" {
				t.Error("User ID not properly set in context")
			}
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
	*/
}
