# Ginx - Functional Middleware System for Gin

A functional middleware system for the Gin web framework.

## Features

- **Zero-config usage** - Ready to use with sensible defaults
- **Functional composition** - Chain middleware with conditions
- **High performance** - Optimized for production workloads
- **Security-first** - Built-in authentication, authorization, and CORS
- **Production-ready** - Comprehensive logging, recovery, and metrics
- **Conditional execution** - Fine-grained control over middleware application

## Quick Start

### Installation

```bash
go get github.com/simp-lee/ginx
```

### Basic Usage

```go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/simp-lee/ginx"
)

func main() {
    r := gin.New()

    // Zero-config middleware stack
    r.Use(
        ginx.Recovery(),
        ginx.Logger(),
        ginx.Timeout(),
        ginx.CORS(),
        ginx.RateLimit(100, 200), // 100 RPS, 200 burst
    )

    r.GET("/", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "Hello World"})
    })

    r.Run(":8080")
}
```

## Core Architecture

### Core Types

```go
// Functional middleware type
type Middleware func(gin.HandlerFunc) gin.HandlerFunc

// Condition for conditional middleware execution
type Condition func(*gin.Context) bool

// Generic option pattern
type Option[T any] func(*T)

// Error handler type
type ErrorHandler func(*gin.Context, error)
```

### Middleware Chain

The `Chain` type enables functional composition of middleware with conditions:

```go
type Chain struct {
    middlewares  []Middleware
    errorHandler ErrorHandler
}

// Core methods
func (c *Chain) Use(m Middleware) *Chain                     // Add middleware
func (c *Chain) When(cond Condition, m Middleware) *Chain    // Conditional middleware
func (c *Chain) Unless(cond Condition, m Middleware) *Chain  // Inverse conditional
func (c *Chain) OnError(handler ErrorHandler) *Chain         // Error handling
func (c *Chain) Build() gin.HandlerFunc                      // Build final handler
```

## Conditional Middleware

### Logical Operators

```go
// Logical composition
func And(conds ...Condition) Condition   // All conditions must be true
func Or(conds ...Condition) Condition    // Any condition must be true
func Not(cond Condition) Condition       // Negate condition
```

### Path Conditions

```go
func PathIs(paths ...string) Condition           // Exact path match
func PathHasPrefix(prefix string) Condition      // Path prefix match
func PathHasSuffix(suffix string) Condition      // Path suffix match
func PathMatches(pattern string) Condition       // Regex path match
```

### HTTP Conditions

```go
func MethodIs(methods ...string) Condition           // HTTP method match
func HeaderExists(key string) Condition              // Header existence
func HeaderEquals(key, value string) Condition       // Header value match
func ContentTypeIs(contentTypes ...string) Condition // Content-Type match
```

### Custom Conditions

```go
func Custom(fn func(*gin.Context) bool) Condition // Custom condition function
```

## Advanced Usage

### Conditional Chain Composition

```go
func setupMiddleware() gin.HandlerFunc {
    // Define complex conditions
    isProtectedAPI := ginx.And(
        ginx.PathHasPrefix("/api/"),
        ginx.Not(ginx.PathIs("/api/login", "/api/register")),
    )

    isAdminAPI := ginx.And(
        ginx.HeaderExists("Authorization"),
        ginx.PathHasPrefix("/api/admin/"),
    )

    // Build conditional middleware chain
    chain := ginx.NewChain().
        OnError(func(c *gin.Context, err error) {
            c.JSON(500, gin.H{"error": err.Error()})
        }).
        Use(ginx.Recovery()).
        Use(ginx.Logger()).
        When(ginx.PathHasPrefix("/api/heavy"),
            ginx.Timeout(ginx.WithTimeout(60*time.Second))).
        When(ginx.Not(ginx.PathIs("/health")),
            ginx.RateLimit(100, 200)).
        When(isProtectedAPI, ginx.Auth(jwtService)).
        When(isAdminAPI, ginx.RequireRolePermission(rbacService, "admin", "read")).
        Unless(ginx.MethodIs("OPTIONS"), ginx.CORS())

    return chain.Build()
}
```

## Available Middleware

### Authentication & Authorization

#### JWT Authentication
```go
// JWT authentication middleware
ginx.Auth(jwtService)

// Extract user information from context
userID := ginx.GetUserID(c)        // Get authenticated user ID
userRoles := ginx.GetUserRoles(c)  // Get user roles
```

#### RBAC Authorization
```go
// Role-based access control
ginx.RequirePermission(rbacService, "resource", "action")        // Combined check
ginx.RequireRolePermission(rbacService, "resource", "action")    // Role-only check
ginx.RequireUserPermission(rbacService, "resource", "action")    // User-only check

// Condition functions for complex logic
ginx.IsAuthenticated()                                 // Check if user is authenticated
ginx.HasPermission(rbacService, "resource", "action")  // Check permission as condition
```

### Traffic Management

#### Rate Limiting
```go
// Basic rate limiting (100 RPS, 200 burst)
ginx.RateLimit(100, 200)

// IP-based rate limiting
ginx.RateLimitByIP(100, 200)

// User-based rate limiting
ginx.RateLimitByUser(100, 200, userKeyFunc)

// Path-based rate limiting
ginx.RateLimitByPath(100, 200)

// Rate limiting with wait instead of reject
ginx.RateLimitWithWait(100, 200, maxWait)

// Advanced configuration
rateLimiter := ginx.NewRateLimiter(100, 200,
    ginx.WithSkipFunc(func(c *gin.Context) bool {
        return c.GetHeader("X-Skip-Rate-Limit") == "true"
    }),
    ginx.WithKeyFunc(func(c *gin.Context) string {
        return c.ClientIP() + ":" + c.Request.URL.Path
    }),
    ginx.WithHeaders(true), // Add X-RateLimit-* headers
)
ginx.RateLimitWith(rateLimiter)
```

#### Caching
```go
// Basic response caching
ginx.Cache(cacheService)

// Grouped caching for better organization
ginx.CacheWithGroup(cacheService, "api-responses")
```

### Security

#### CORS
```go
// Development CORS (allows all origins - development only!)
ginx.CORSDefault()

// Production CORS with explicit configuration
ginx.CORS(
    ginx.WithAllowOrigins("https://example.com", "https://app.example.com"),
    ginx.WithAllowMethods("GET", "POST", "PUT", "DELETE"),
    ginx.WithAllowHeaders("Content-Type", "Authorization"),
    ginx.WithExposeHeaders("X-Total-Count"),
    ginx.WithAllowCredentials(true),
    ginx.WithMaxAge(86400), // 24 hours
)
```

### Observability

#### Logging
```go
// Structured request logging
ginx.Logger(loggerOptions...)

// Logs include: method, path, query, status, latency, IP, user agent, size, protocol, referer
// Automatic log levels: ERROR (5xx), WARN (4xx), INFO (2xx-3xx)
```

#### Recovery
```go
// Basic panic recovery
ginx.Recovery()

// Recovery with custom handler
ginx.RecoveryWith(func(c *gin.Context, err any) {
    log.Printf("Panic recovered: %v", err)
    c.JSON(500, gin.H{"error": "Internal server error"})
})
```

#### Timeout
```go
// Default 30-second timeout
ginx.Timeout()

// Custom timeout configuration
ginx.Timeout(
    ginx.WithTimeout(60*time.Second),
    ginx.WithTimeoutResponse(gin.H{
        "error": "Request timeout",
        "code": "TIMEOUT",
    }),
)
```

## Configuration Examples

### Production API Setup

```go
func setupProductionAPI(jwtService jwt.Service, rbacService rbac.Service, cache cache.Cache) gin.HandlerFunc {
    return ginx.NewChain().
        OnError(func(c *gin.Context, err error) {
            log.Printf("Middleware error: %v", err)
            c.JSON(500, gin.H{"error": "Internal server error"})
        }).
        Use(ginx.Recovery()).
        Use(ginx.Logger()).
        Use(ginx.Timeout(ginx.WithTimeout(30*time.Second))).
        Use(ginx.CORS(
            ginx.WithAllowOrigins("https://yourdomain.com"),
            ginx.WithAllowMethods("GET", "POST", "PUT", "DELETE", "OPTIONS"),
            ginx.WithAllowHeaders("Content-Type", "Authorization"),
            ginx.WithAllowCredentials(true),
        )).
        When(ginx.Not(ginx.PathIs("/health", "/metrics")),
            ginx.RateLimitByIP(100, 200)).
        When(ginx.PathHasPrefix("/api/"),
            ginx.Auth(jwtService)).
        When(ginx.And(
            ginx.PathHasPrefix("/api/admin/"),
            ginx.Not(ginx.MethodIs("OPTIONS")),
        ), ginx.RequireRolePermission(rbacService, "admin", "access")).
        When(ginx.And(
            ginx.MethodIs("GET"),
            ginx.PathHasPrefix("/api/"),
        ), ginx.Cache(cache)).
        Build()
}
```

### Microservice Setup

```go
func setupMicroservice() gin.HandlerFunc {
    return ginx.NewChain().
        Use(ginx.Recovery()).
        Use(ginx.Logger()).
        Use(ginx.Timeout(ginx.WithTimeout(10*time.Second))).
        When(ginx.PathHasPrefix("/internal/"),
            ginx.RateLimitByPath(1000, 2000)).
        When(ginx.PathHasPrefix("/external/"),
            ginx.RateLimitByIP(10, 20)).
        Unless(ginx.PathIs("/health"),
            ginx.RequestID()).
        Build()
}
```

## Performance

Ginx is designed for high-performance production environments:

- **Zero allocation** conditional execution
- **Token bucket** rate limiting with `golang.org/x/time/rate`
- **Sharded caching** for reduced lock contention
- **Memory-efficient** middleware chaining
- **Automatic cleanup** of expired rate limiters

## Dependencies

- **Core**: `github.com/gin-gonic/gin` - Gin web framework
- **Rate Limiting**: `golang.org/x/time/rate` - Token bucket implementation
- **JWT**: `github.com/simp-lee/jwt` - JWT authentication service
- **RBAC**: `github.com/simp-lee/rbac` - Role-based access control
- **Logging**: `github.com/simp-lee/logger` - Structured logging
- **Caching**: `github.com/simp-lee/cache` - Sharded cache implementation

## License

MIT License.