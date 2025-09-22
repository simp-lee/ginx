package ginx

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/rbac"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock RBAC Service for testing
type MockRBACService struct {
	mock.Mock
}

func (m *MockRBACService) CreateRole(roleID, name, description string) error {
	args := m.Called(roleID, name, description)
	return args.Error(0)
}

func (m *MockRBACService) GetRole(roleID string) (*rbac.Role, error) {
	args := m.Called(roleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*rbac.Role), args.Error(1)
}

func (m *MockRBACService) UpdateRole(roleID, name, description string) error {
	args := m.Called(roleID, name, description)
	return args.Error(0)
}

func (m *MockRBACService) DeleteRole(roleID string) error {
	args := m.Called(roleID)
	return args.Error(0)
}

func (m *MockRBACService) ListRoles() ([]*rbac.Role, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*rbac.Role), args.Error(1)
}

func (m *MockRBACService) RoleExists(roleID string) (bool, error) {
	args := m.Called(roleID)
	return args.Bool(0), args.Error(1)
}

func (m *MockRBACService) AssignRole(userID, roleID string) error {
	args := m.Called(userID, roleID)
	return args.Error(0)
}

func (m *MockRBACService) UnassignRole(userID, roleID string) error {
	args := m.Called(userID, roleID)
	return args.Error(0)
}

func (m *MockRBACService) GetUserRoles(userID string) ([]string, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockRBACService) GetRoleUsers(roleID string) ([]string, error) {
	args := m.Called(roleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockRBACService) UserHasRole(userID, roleID string) (bool, error) {
	args := m.Called(userID, roleID)
	return args.Bool(0), args.Error(1)
}

func (m *MockRBACService) AddRolePermission(roleID, resource, action string) error {
	args := m.Called(roleID, resource, action)
	return args.Error(0)
}

func (m *MockRBACService) AddRolePermissions(roleID, resource string, actions []string) error {
	args := m.Called(roleID, resource, actions)
	return args.Error(0)
}

func (m *MockRBACService) RemoveRolePermission(roleID, resource, action string) error {
	args := m.Called(roleID, resource, action)
	return args.Error(0)
}

func (m *MockRBACService) RemoveRolePermissions(roleID, resource string) error {
	args := m.Called(roleID, resource)
	return args.Error(0)
}

func (m *MockRBACService) GetRolePermissions(roleID string) (map[string][]string, error) {
	args := m.Called(roleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string][]string), args.Error(1)
}

func (m *MockRBACService) AddUserPermission(userID, resource, action string) error {
	args := m.Called(userID, resource, action)
	return args.Error(0)
}

func (m *MockRBACService) AddUserPermissions(userID, resource string, actions []string) error {
	args := m.Called(userID, resource, actions)
	return args.Error(0)
}

func (m *MockRBACService) RemoveUserPermission(userID, resource, action string) error {
	args := m.Called(userID, resource, action)
	return args.Error(0)
}

func (m *MockRBACService) RemoveUserPermissions(userID, resource string) error {
	args := m.Called(userID, resource)
	return args.Error(0)
}

func (m *MockRBACService) RemoveAllUserPermissions(userID string) error {
	args := m.Called(userID)
	return args.Error(0)
}

func (m *MockRBACService) GetUserPermissions(userID string) (map[string][]string, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string][]string), args.Error(1)
}

func (m *MockRBACService) HasPermission(userID, resource, action string) (bool, error) {
	args := m.Called(userID, resource, action)
	return args.Bool(0), args.Error(1)
}

func (m *MockRBACService) HasRolePermission(userID, resource, action string) (bool, error) {
	args := m.Called(userID, resource, action)
	return args.Bool(0), args.Error(1)
}

func (m *MockRBACService) HasUserPermission(userID, resource, action string) (bool, error) {
	args := m.Called(userID, resource, action)
	return args.Bool(0), args.Error(1)
}

func (m *MockRBACService) GetUserAllPermissions(userID string) (map[string][]string, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string][]string), args.Error(1)
}

func (m *MockRBACService) CheckMultiplePermissions(userID string, permissions []rbac.Permission) (map[rbac.Permission]bool, error) {
	args := m.Called(userID, permissions)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[rbac.Permission]bool), args.Error(1)
}

func (m *MockRBACService) Stats() rbac.Stats {
	args := m.Called()
	return args.Get(0).(rbac.Stats)
}

func (m *MockRBACService) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestGetUserIDOrAbort(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return user ID when authenticated", func(t *testing.T) {
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		userID, ok := GetUserIDOrAbort(c)
		assert.True(t, ok)
		assert.Equal(t, "user123", userID)
		assert.Equal(t, http.StatusOK, w.Code) // Should not abort
	})

	t.Run("should abort with 401 when not authenticated", func(t *testing.T) {
		c, w := TestContext("GET", "/test", nil)
		// Don't set user_id in context

		userID, ok := GetUserIDOrAbort(c)
		assert.False(t, ok)
		assert.Equal(t, "", userID)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "user not authenticated", response["error"])
	})

	t.Run("should abort when user_id is not string", func(t *testing.T) {
		c, w := TestContext("GET", "/test", nil)
		c.Set("user_id", 12345) // Not a string

		userID, ok := GetUserIDOrAbort(c)
		assert.False(t, ok)
		assert.Equal(t, "", userID)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestRequirePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should allow access when user has permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasPermission", "user123", "posts", "read").Return(true, nil)

		middleware := RequirePermission(mockRBAC, "posts", "read")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return 403 when user lacks permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasPermission", "user123", "posts", "delete").Return(false, nil)

		middleware := RequirePermission(mockRBAC, "posts", "delete")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusForbidden, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "permission denied", response["error"])

		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return 401 when user not authenticated", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		// Don't set user_id in context

		middleware := RequirePermission(mockRBAC, "posts", "read")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "user not authenticated", response["error"])

		// Should not call RBAC service
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return 500 when permission check fails", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasPermission", "user123", "posts", "read").Return(false, errors.New("database error"))

		middleware := RequirePermission(mockRBAC, "posts", "read")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "permission check failed", response["error"])

		mockRBAC.AssertExpectations(t)
	})
}

func TestRequireRolePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should allow access when user has role permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasRolePermission", "user123", "admin", "access").Return(true, nil)

		middleware := RequireRolePermission(mockRBAC, "admin", "access")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return 403 when user lacks role permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasRolePermission", "user123", "admin", "access").Return(false, nil)

		middleware := RequireRolePermission(mockRBAC, "admin", "access")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusForbidden, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "insufficient role permissions", response["error"])

		mockRBAC.AssertExpectations(t)
	})
}

func TestRequireUserPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should allow access when user has direct permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasUserPermission", "user123", "users", "update").Return(true, nil)

		middleware := RequireUserPermission(mockRBAC, "users", "update")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return 403 when user lacks direct permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, w := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasUserPermission", "user123", "users", "delete").Return(false, nil)

		middleware := RequireUserPermission(mockRBAC, "users", "delete")
		handler := middleware(func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true})
		})

		handler(c)

		assert.Equal(t, http.StatusForbidden, w.Code)

		var response map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "insufficient user permissions", response["error"])

		mockRBAC.AssertExpectations(t)
	})
}

func TestIsAuthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return true when user is authenticated", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		condition := IsAuthenticated()
		result := condition(c)

		assert.True(t, result)
	})

	t.Run("should return false when user is not authenticated", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)
		// Don't set user_id

		condition := IsAuthenticated()
		result := condition(c)

		assert.False(t, result)
	})

	t.Run("should return false when user_id is not string", func(t *testing.T) {
		c, _ := TestContext("GET", "/test", nil)
		c.Set("user_id", 12345) // Not a string

		condition := IsAuthenticated()
		result := condition(c)

		assert.False(t, result)
	})
}

func TestHasPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return true when user has permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasPermission", "user123", "posts", "read").Return(true, nil)

		condition := HasPermission(mockRBAC, "posts", "read")
		result := condition(c)

		assert.True(t, result)
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return false when user lacks permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasPermission", "user123", "posts", "delete").Return(false, nil)

		condition := HasPermission(mockRBAC, "posts", "delete")
		result := condition(c)

		assert.False(t, result)
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return false when user not authenticated", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		// Don't set user_id

		condition := HasPermission(mockRBAC, "posts", "read")
		result := condition(c)

		assert.False(t, result)
		// Should not call RBAC service
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return false when permission check fails", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasPermission", "user123", "posts", "read").Return(false, errors.New("database error"))

		condition := HasPermission(mockRBAC, "posts", "read")
		result := condition(c)

		assert.False(t, result)
		mockRBAC.AssertExpectations(t)
	})
}

func TestHasRolePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return true when user has role permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasRolePermission", "user123", "admin", "access").Return(true, nil)

		condition := HasRolePermission(mockRBAC, "admin", "access")
		result := condition(c)

		assert.True(t, result)
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return false when user lacks role permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasRolePermission", "user123", "admin", "access").Return(false, nil)

		condition := HasRolePermission(mockRBAC, "admin", "access")
		result := condition(c)

		assert.False(t, result)
		mockRBAC.AssertExpectations(t)
	})
}

func TestHasUserPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should return true when user has direct permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasUserPermission", "user123", "users", "update").Return(true, nil)

		condition := HasUserPermission(mockRBAC, "users", "update")
		result := condition(c)

		assert.True(t, result)
		mockRBAC.AssertExpectations(t)
	})

	t.Run("should return false when user lacks direct permission", func(t *testing.T) {
		mockRBAC := new(MockRBACService)
		c, _ := TestContext("GET", "/test", nil)
		SetUserID(c, "user123")

		mockRBAC.On("HasUserPermission", "user123", "users", "delete").Return(false, nil)

		condition := HasUserPermission(mockRBAC, "users", "delete")
		result := condition(c)

		assert.False(t, result)
		mockRBAC.AssertExpectations(t)
	})
}

// Integration test scenarios
func TestRBACMiddlewareIntegration(t *testing.T) {
	t.Skip("Skipping integration test - requires real RBAC service")

	// This test would use a real RBAC service for integration testing
	// Uncomment and modify when testing with actual RBAC service

	/*
		rbacService, err := rbac.New()
		if err != nil {
			t.Fatal("Failed to create RBAC service:", err)
		}

		// Setup test data
		rbacService.CreateRole("admin", "Administrator", "Full access")
		rbacService.CreateRole("user", "User", "Limited access")

		rbacService.AddRolePermissions("admin", "users", []string{"create", "read", "update", "delete"})
		rbacService.AddRolePermissions("user", "users", []string{"read"})

		rbacService.AssignRole("admin123", "admin")
		rbacService.AssignRole("user123", "user")

		t.Run("admin should have all permissions", func(t *testing.T) {
			c, w := TestContext("GET", "/test", nil)
			SetUserID(c, "admin123")

			middleware := RequirePermission(rbacService, "users", "delete")
			handler := middleware(func(c *gin.Context) {
				c.JSON(200, gin.H{"success": true})
			})

			handler(c)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("user should not have delete permission", func(t *testing.T) {
			c, w := TestContext("GET", "/test", nil)
			SetUserID(c, "user123")

			middleware := RequirePermission(rbacService, "users", "delete")
			handler := middleware(func(c *gin.Context) {
				c.JSON(200, gin.H{"success": true})
			})

			handler(c)

			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	*/
}
