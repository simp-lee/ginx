package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
	"github.com/simp-lee/jwt"
	"github.com/simp-lee/rbac"
)

func main() {
	r := gin.Default()

	// 创建JWT服务
	jwtService, err := jwt.New("your-super-secret-key-here",
		jwt.WithLeeway(5*time.Minute),
		jwt.WithIssuer("ginx-app"),
		jwt.WithMaxTokenLifetime(24*time.Hour),
	)
	if err != nil {
		log.Fatal("Failed to create JWT service:", err)
	}

	// 创建RBAC服务
	rbacService, err := rbac.New() // 默认内存存储
	if err != nil {
		log.Fatal("Failed to create RBAC service:", err)
	}
	setupRBAC(rbacService)

	// 极简架构：条件组合中间件链
	isAPIPath := ginx.PathHasPrefix("/api/")
	isPublicPath := ginx.Or(ginx.PathIs("/api/login", "/health"))
	isAdminPath := ginx.PathHasPrefix("/api/admin/")
	isUserPath := ginx.PathHasPrefix("/api/users/")

	// 构建中间件链
	chain := ginx.NewChain().
		Use(ginx.Recovery()).
		Use(ginx.Logger()).
		// 条件认证：API路径且非公开路径需要JWT认证
		When(ginx.And(isAPIPath, ginx.Not(isPublicPath)),
			ginx.Auth(jwtService)).
		// 管理员区域需要管理权限
		When(isAdminPath,
			ginx.RequirePermission(rbacService, "admin", "access")).
		// 用户区域需要用户读取权限
		When(isUserPath,
			ginx.RequirePermission(rbacService, "users", "read")).
		Build()

	r.Use(chain)

	// 公开端点
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.POST("/api/login", loginHandler(jwtService))
	r.POST("/api/refresh", refreshHandler(jwtService))

	// 需要认证的用户端点
	r.GET("/api/users", listUsers)

	// 需要特定权限的操作 - 使用条件中间件
	r.POST("/api/users",
		ginx.RequirePermission(rbacService, "users", "create")(createUser))

	r.PUT("/api/users/:id",
		ginx.RequirePermission(rbacService, "users", "update")(updateUser))

	r.DELETE("/api/users/:id",
		ginx.RequirePermission(rbacService, "users", "delete")(deleteUser))

	// 管理员端点
	r.GET("/api/admin/stats", adminStats)
	r.DELETE("/api/admin/users/:id", adminDeleteUser)

	log.Println("Server running on :8080")
	r.Run(":8080")
}

// 登录处理器
func loginHandler(jwtService jwt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "invalid request"})
			return
		}

		// 简化的用户验证
		var userID string
		var roles []string

		switch req.Username {
		case "admin":
			if req.Password != "admin123" {
				c.JSON(401, gin.H{"error": "invalid credentials"})
				return
			}
			userID = "admin"
			roles = []string{"admin"}
		case "user":
			if req.Password != "user123" {
				c.JSON(401, gin.H{"error": "invalid credentials"})
				return
			}
			userID = "user123"
			roles = []string{"user"}
		default:
			c.JSON(401, gin.H{"error": "invalid credentials"})
			return
		}

		// 生成JWT token
		token, err := jwtService.GenerateToken(userID, roles, 24*time.Hour)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to generate token"})
			return
		}

		c.JSON(200, gin.H{
			"token":   token,
			"user_id": userID,
			"roles":   roles,
			"expires": time.Now().Add(24 * time.Hour),
		})
	}
}

// 刷新token处理器 - 展示原生库的RefreshToken接口使用
func refreshHandler(jwtService jwt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "invalid request"})
			return
		}

		// 使用原生库的RefreshToken方法
		newToken, err := jwtService.RefreshToken(req.Token)
		if err != nil {
			c.JSON(401, gin.H{"error": "failed to refresh token"})
			return
		}

		c.JSON(200, gin.H{
			"token": newToken,
		})
	}
}

func createUser(c *gin.Context) {
	c.JSON(201, gin.H{"message": "user created"})
}

func updateUser(c *gin.Context) {
	userID := c.Param("id")
	c.JSON(200, gin.H{"message": "user " + userID + " updated"})
}

func deleteUser(c *gin.Context) {
	userID := c.Param("id")
	c.JSON(200, gin.H{"message": "user " + userID + " deleted"})
}

// 基础处理函数
func listUsers(c *gin.Context) {
	c.JSON(200, gin.H{
		"users": []gin.H{
			{"id": "1", "name": "John"},
			{"id": "2", "name": "Jane"},
		},
	})
}

func adminStats(c *gin.Context) {
	c.JSON(200, gin.H{
		"total_users":  100,
		"active_users": 85,
	})
}

func adminDeleteUser(c *gin.Context) {
	userID := c.Param("id")
	c.JSON(200, gin.H{"message": "admin deleted user " + userID})
}

// 设置RBAC权限数据
func setupRBAC(service rbac.Service) {
	// 创建角色
	service.CreateRole("admin", "Administrator", "Full system access")
	service.CreateRole("user", "Regular User", "Basic user access")

	// 设置角色权限
	service.AddRolePermissions("admin", "admin", []string{"access"})
	service.AddRolePermissions("admin", "users", []string{"create", "read", "update", "delete"})
	service.AddRolePermissions("user", "users", []string{"read"})

	// 分配角色给用户
	service.AssignRole("admin", "admin")
	service.AssignRole("user123", "user")

	// 可选：直接给用户分配权限（绕过角色）
	// service.AddUserPermissions("special_user", "users", []string{"update"})
}
