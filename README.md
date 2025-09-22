# Ginx - Functional Middleware for Gin

Minimal, composable, and high-performance middleware toolkit for Gin, with conditional execution and functional chaining.

## Features

- Functional composition: Chain + Condition to precisely control execution
- Production-ready: recovery, logging, timeout, CORS, auth, RBAC, cache, rate limit
- High performance: zero-allocation conditions, token-bucket rate limiting, sharded cache
- Clean API: unified Option/Condition pattern, easy to extend

## Installation

```bash
go get github.com/simp-lee/ginx
```

## Quick Start

```go
package main

import (
    "time"
    "github.com/gin-gonic/gin"
    "github.com/simp-lee/ginx"
)

func main() {
    r := gin.New()

    // Basic middleware stack
    r.Use(
        ginx.Recovery(),                        // Panic protection with logging
        ginx.Logger(),                          // Structured request logging  
        ginx.Timeout(),                         // 30s timeout protection
        ginx.CORS(ginx.WithAllowOrigins("*")),  // CORS for development
        ginx.RateLimit(100, 200),               // 100 RPS, 200 burst per IP
    )

    r.GET("/", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "Hello World"})
    })
    
    r.GET("/slow", func(c *gin.Context) {
        // This will timeout after 30 seconds due to Timeout middleware
        time.Sleep(35 * time.Second)
        c.JSON(200, gin.H{"message": "This won't be reached"})
    })

    r.Run(":8080")
}
```

### Conditional Middleware

```go
// Build conditional middleware chain
chain := ginx.NewChain().
    Use(ginx.Recovery()).
    Use(ginx.Logger()).
    // Apply rate limiting only to API routes
    When(ginx.PathHasPrefix("/api/"), ginx.RateLimit(100, 200)).
    // Apply CORS only to browser requests  
    When(ginx.HeaderExists("Origin"), ginx.CORS(ginx.WithAllowOrigins("*"))).
    // Longer timeout for heavy operations
    When(ginx.PathHasPrefix("/api/heavy/"), ginx.Timeout(ginx.WithTimeout(60*time.Second)))

r.Use(chain.Build())
```

## Core Concepts

```go
type Middleware func(gin.HandlerFunc) gin.HandlerFunc
type Condition  func(*gin.Context) bool
type Option[T any] func(*T)
type ErrorHandler func(*gin.Context, error)
```

### Chain (functional composition)

Chain provides fluent API for building middleware chains with conditional execution and error handling.

**Chain methods:**
- `NewChain()` - Create new chain builder
- `Use(m Middleware)` - Add middleware unconditionally  
- `When(cond Condition, m Middleware)` - Add middleware if condition is true
- `Unless(cond Condition, m Middleware)` - Add middleware if condition is false
- `OnError(handler ErrorHandler)` - Set error handler for chain execution
- `Build()` - Build final `gin.HandlerFunc`

Note:
- `OnError` is invoked only when `c.Errors` is non-empty. To have errors handled by the chain-level handler, call `c.Error(err)` in your middleware or handlers.

**Example:**
```go
chain := ginx.NewChain().
  OnError(func(c *gin.Context, err error) { c.JSON(500, gin.H{"error": err.Error()}) }).
  Use(ginx.Recovery()).
  Use(ginx.Logger()).
  When(ginx.PathHasPrefix("/api/heavy"), ginx.Timeout(ginx.WithTimeout(60*time.Second))).
  Unless(ginx.PathIs("/health"), ginx.RateLimit(100, 200))

r.Use(chain.Build())
```

### Conditions

Conditions are lightweight functions of type `func(*gin.Context) bool` used to decide whether middleware should execute. Most conditions are zero-allocation; `ContentTypeIs` parses MIME types (slight cost), and `PathMatches` compiles regex once at condition creation.

**Logic combinators:**
- `And(conds ...Condition)` - All conditions must be true
- `Or(conds ...Condition)` - At least one condition is true
- `Not(cond Condition)` - Condition must be false

**Path conditions:**
- `PathIs(paths ...string)` - Exact path match
- `PathHasPrefix(prefix string)` - Path starts with prefix
- `PathHasSuffix(suffix string)` - Path ends with suffix  
- `PathMatches(pattern string)` - Path matches regex pattern

**HTTP conditions:**
- `MethodIs(methods ...string)` - HTTP method matches
- `HeaderExists(key string)` - Request header exists
- `HeaderEquals(key, value string)` - Header equals exact value
- `ContentTypeIs(types ...string)` - Content-Type matches (MIME parsing)

**Custom conditions:**
- `Custom(fn func(*gin.Context) bool)` - Custom condition function
- `OnTimeout()` - Request has timed out

**RBAC conditions (require auth):**
- `IsAuthenticated()` - User is authenticated
- `HasPermission(service rbac.Service, resource, action string)` - Combined role + user permissions
- `HasRolePermission(service rbac.Service, resource, action string)` - Role-based permissions only
- `HasUserPermission(service rbac.Service, resource, action string)` - Direct user permissions only

## Middleware Overview

### Recovery (panic protection)

Graceful panic recovery middleware with intelligent error handling and structured logging.

**Usage:**
- `Recovery(loggerOptions...)` - Basic recovery with default handler
- `RecoveryWith(handler RecoveryHandler, loggerOptions...)` - Custom recovery handler

**Types:**
```go
type RecoveryHandler func(*gin.Context, any)
```

**Features:**
- **Smart error detection**: Distinguishes between panics and broken pipe errors
- **Structured logging**: Uses `github.com/simp-lee/logger` with configurable options
- **Clean stack traces**: Filters out recovery middleware frames and runtime panic calls
- **Broken pipe handling**: Special treatment for client disconnections (warns without stack trace)
- **Custom responses**: Configurable error response format via recovery handler

**Default behavior:**
- Panics: Logs error + full stack trace, returns 500 JSON response
- Broken pipes: Logs warning without stack trace, aborts connection gracefully

**Example:**
```go
// Basic recovery with default handler
ginx.Recovery()

// Custom recovery handler with structured response
ginx.RecoveryWith(func(c *gin.Context, err any) {
    c.JSON(500, gin.H{
        "error": "Internal Server Error", 
        "request_id": c.GetString("request_id"),
        "timestamp": time.Now().Unix(),
    })
}, logger.WithLevel(slog.LevelError), logger.WithConsole(true))
```

### Logger (structured logs)

Structured HTTP request logging middleware with configurable log levels and comprehensive request metadata.

**Usage:**
- `Logger(loggerOptions...)` - HTTP request logger with configurable options

**Features:**
- **Smart log levels**: Automatic level based on status code (5xx=Error, 4xx=Warn, others=Info)
- **Rich metadata**: Method, path, query, status, latency, IP, user agent, size, protocol, referer
- **Error tracking**: Separate error logging for gin context errors (when present)
- **Structured format**: Uses `github.com/simp-lee/logger` with key-value pairs
- **Performance optimized**: Single timer measurement, minimal allocations
- **Client IP detection**: Uses Gin's `ClientIP()` method (supports proxy headers)

**Example:**
```go
// Basic logging with default configuration
ginx.Logger()

// Custom log level configuration
ginx.Logger(logger.WithLevel(slog.LevelDebug), logger.WithConsole(true))
```

### Timeout

Context-based request timeout middleware with buffered response handling to prevent partial responses.

**Usage:**
- `Timeout(options...)` - Request timeout middleware with configurable options

**Options:**
- `WithTimeout(duration)` - Set timeout duration (default: 30 seconds)
- `WithTimeoutResponse(response)` - Set custom timeout response (default: JSON with code 408). If the value cannot be JSON-serialized, it will automatically fall back to the default 408 response.
- `WithTimeoutMessage(message)` - Set timeout message (creates JSON response with code 408)

**Features:**
- **Atomic response handling**: Buffered writer prevents partial responses during timeout
- **Context cancellation**: Proper request context timeout with cancellation
- **Timeout detection**: Sets `X-Timeout: true` header for conditional middleware
- **Zero timeout support**: Immediate timeout response for zero/negative durations

**Helpers:**
- `IsTimeout(c *gin.Context) bool` - Check if request timed out
- Condition `OnTimeout()` - For conditional middleware on timeout responses

**Example:**
```go
// Different timeouts for different endpoints
chain := ginx.NewChain().
    When(ginx.PathHasPrefix("/api/heavy"), 
        ginx.Timeout(ginx.WithTimeout(60*time.Second))).
    Unless(ginx.PathIs("/health"), 
        ginx.Timeout(ginx.WithTimeout(5*time.Second)))
```

### CORS

Cross-Origin Resource Sharing (CORS) middleware with security-first design and proper preflight handling.

**Usage:**
- `CORS(options...)` - CORS middleware with explicit origin configuration (required)
- `CORSDefault()` - Development-only helper (allows all origins)

**Options:**
- `WithAllowOrigins(origins...)` - Set allowed origins (**required**, no default)
- `WithAllowMethods(methods...)` - Set allowed HTTP methods (default: GET, POST, PUT, DELETE, OPTIONS)
- `WithAllowHeaders(headers...)` - Set allowed request headers (default: Content-Type, Authorization, Cache-Control, X-Requested-With)
- `WithExposeHeaders(headers...)` - Set headers exposed to client (default: none)
- `WithAllowCredentials(allow bool)` - Allow credentials like cookies/auth headers (default: false)
- `WithMaxAge(duration)` - Set preflight cache duration (default: 12 hours)

**Security features:**
- **Explicit origins required**: No default origins for security
- **Credentials validation**: Prevents wildcard origins with credentials (runtime panic)
- **Proper preflight handling**: Full OPTIONS request validation
- **Vary headers**: Prevents proxy cache pollution

**Example:**
```go
// Development: Allow all origins (use with caution)
ginx.CORS(ginx.WithAllowOrigins("*"))

// Production: Explicit security configuration
ginx.CORS(
    ginx.WithAllowOrigins("https://example.com", "https://app.example.com"),
    ginx.WithAllowHeaders("Content-Type", "Authorization"),
    ginx.WithAllowCredentials(true),
)
```

**Security note**: `WithAllowCredentials(true)` cannot be used with wildcard origin `"*"` (enforced at runtime).

### Auth (JWT)

JWT authentication middleware with flexible token extraction and comprehensive context integration.

**Usage:**
- `Auth(jwtService jwt.Service)` - JWT authentication middleware

**Features:**
- **Flexible token extraction**: Supports both `Authorization: Bearer <token>` header and `?token=<token>` query parameter
- **Automatic context population**: Sets user ID, roles, and token metadata in gin context
- **Type-safe context keys**: Uses typed context keys to prevent conflicts
- **Validation & parsing**: Uses `jwtService.ValidateAndParse()` for comprehensive token validation

**Context helpers (getters):**
- `GetUserID(c) (string, bool)` - Get authenticated user ID
- `GetUserRoles(c) ([]string, bool)` - Get user roles from token
- `GetTokenID(c) (string, bool)` - Get JWT token ID
- `GetTokenExpiresAt(c) (time.Time, bool)` - Get token expiration time
- `GetTokenIssuedAt(c) (time.Time, bool)` - Get token issued time
- `GetUserIDOrAbort(c) (string, bool)` - Get user ID or abort with 401 if not authenticated

**Context helpers (setters):**
- `SetUserID(c, userID string)` - Set user ID in context
- `SetUserRoles(c, roles []string)` - Set user roles in context
- `SetTokenID(c, tokenID string)` - Set token ID in context
- `SetTokenExpiresAt(c, expiresAt time.Time)` - Set token expiration
- `SetTokenIssuedAt(c, issuedAt time.Time)` - Set token issued time

**Example:**
```go
jwtService, _ := jwt.New("secret-key", jwt.WithLeeway(5*time.Minute))

// Protect API routes with JWT
r.Use(ginx.NewChain().
    When(ginx.PathHasPrefix("/api/"), ginx.Auth(jwtService)).
    Build())
```

### RBAC (Role-Based Access Control)

Role-based access control middleware with fine-grained permission checking and condition support.

**Usage:**
- Middlewares (require authentication):
  - `RequirePermission(service rbac.Service, resource, action string)` - Check combined role + user permissions
  - `RequireRolePermission(service rbac.Service, resource, action string)` - Check role-based permissions only  
  - `RequireUserPermission(service rbac.Service, resource, action string)` - Check direct user permissions only

**Features:**
- **Three permission models**: Combined, role-only, and user-only permission checking
- **Automatic authentication check**: Uses `GetUserIDOrAbort()` for user validation
- **Detailed error responses**: Distinguishes between permission check failures (500) and access denied (403)
- **Integration with Auth**: Seamlessly works with JWT authentication middleware

**Conditions (for conditional middleware):**
- `IsAuthenticated()` - Check if user is authenticated (no service required)
- `HasPermission(service rbac.Service, resource, action string)` - Check combined permissions
- `HasRolePermission(service rbac.Service, resource, action string)` - Check role permissions
- `HasUserPermission(service rbac.Service, resource, action string)` - Check user permissions

**Error handling:**
- **500 Internal Server Error**: Permission check failed (service error)
- **403 Forbidden**: Permission denied (access not allowed)
- **401 Unauthorized**: User not authenticated (handled by `GetUserIDOrAbort`)

**Example:**
```go
rbacService, _ := rbac.New()

// Require admin permissions for admin routes
r.Use(ginx.NewChain().
    When(ginx.PathHasPrefix("/api/admin/"), 
        ginx.RequireRolePermission(rbacService, "admin", "access")).
    Build())
```

### Cache (response caching)

HTTP-compliant response caching middleware with intelligent cache control and group support.

**Usage:**
- `Cache(cache shardedcache.CacheInterface)` - Cache all cacheable responses (default group)
- `CacheWithGroup(cache shardedcache.CacheInterface, groupName string)` - Cache with group prefix for isolation

**Features:**
- **HTTP-compliant caching**: Respects `Cache-Control: no-store/private` directives
- **Smart exclusions**: Automatically excludes responses with `Set-Cookie` headers to prevent user data leakage
- **2xx-only caching**: Only caches successful responses (200-299 status codes)
- **Efficient cache keys**: Generated from HTTP method, path, and query parameters (`METHOD|PATH?QUERY`)
- **Response reconstruction**: Preserves status code and body, and restores primary headers (multi-value headers are stored as the first value; responses with `Set-Cookie` are not cached)
- **Group isolation**: Optional grouping for cache namespace separation

**Cache key format:**
```
GET|/api/users                    // No query parameters
POST|/api/search?q=test&limit=10  // With query parameters
```

**Example:**
```go
cache := shardedcache.NewCache(shardedcache.Options{
    MaxSize: 1000,
    DefaultExpiration: 5 * time.Minute,
})

// Cache GET requests with version-specific grouping
r.Use(ginx.NewChain().
    When(ginx.And(ginx.MethodIs("GET"), ginx.PathHasPrefix("/api/v1/")), 
        ginx.CacheWithGroup(cache, "api-v1")).
    When(ginx.And(ginx.MethodIs("GET"), ginx.PathHasPrefix("/api/v2/")), 
        ginx.CacheWithGroup(cache, "api-v2")).
    When(ginx.And(
        ginx.MethodIs("GET"), 
        ginx.PathHasPrefix("/api/"),
        ginx.Not(ginx.Or(
            ginx.PathHasPrefix("/api/v1/"),
            ginx.PathHasPrefix("/api/v2/"),
        )),
    ), ginx.Cache(cache)).
    Build())
```

### Rate Limit (token bucket)

High-performance token bucket rate limiting middleware with flexible key generation and dynamic limits.

**Usage:**
- `RateLimit(rps int, burst int, opts ...RateOption)` - Token bucket rate limiting with configurable options

**Key generation options:**
- `WithIP()` - IP-based rate limiting (default behavior)
- `WithUser()` - Per-user rate limiting (requires user context)
- `WithPath()` - Per-path rate limiting (different limits per endpoint)
- `WithKeyFunc(keyFunc func(*gin.Context) string)` - Custom key generation function

**Control options:**
- `WithSkipFunc(skipFunc func(*gin.Context) bool)` - Skip certain requests
- `WithWait(timeout time.Duration)` - Wait for tokens instead of immediate rejection
- `WithDynamicLimits(getLimits func(key string) (rps, burst int))` - Dynamic per-key limits
- `WithStore(store RateLimitStore)` - Custom storage backend (default: shared memory)

**Header options:**
- `WithoutRateLimitHeaders()` - Disable `X-RateLimit-*` headers
- `WithoutRetryAfterHeader()` - Disable `Retry-After` header (enabled by default)

**Features:**
- **Token bucket algorithm**: Smooth rate limiting using `golang.org/x/time/rate`
- **Multiple key strategies**: IP, user ID, path, or custom key generation
- **Dynamic limits**: Per-key rate limits based on user plan, endpoint type, etc.
- **Wait middleware**: Traffic smoothing by waiting for available tokens
- **HTTP compliance**: Standard `X-RateLimit-*` and `Retry-After` headers
- **Thread-safe**: Designed for high-concurrency environments

**HTTP headers:**
```
X-RateLimit-Limit: 100              // Requests per second
X-RateLimit-Remaining: 85            // Available tokens
X-RateLimit-Reset: 1234567890        // Token bucket full reset time (Unix timestamp)
Retry-After: 3                       // Seconds to wait (429 responses only)
```
Note:
- In unlimited mode (both `rps` and `burst` are `<= 0`), no `X-RateLimit-*` headers are returned.

**Resource management:**
- Built-in shared memory store with automatic cleanup
- Call `ginx.CleanupRateLimiters()` on application shutdown for comprehensive cleanup

**Example:**
```go
// Basic IP-based rate limiting: 100 rps, burst 200
r.Use(ginx.RateLimit(100, 200))

// Dynamic per-user limits with wait mode
r.Use(ginx.RateLimit(0, 0,
    ginx.WithUser(),
    ginx.WithWait(2*time.Second),
    ginx.WithDynamicLimits(func(key string) (int, int) {
        if strings.HasPrefix(key, "user:premium_") { 
            return 100, 200  // Premium users
        }
        return 10, 20        // Regular users
    }),
))
```

## Advanced Examples

### Production API Server

```go
package main

import (
    "time"
    "github.com/gin-gonic/gin"
    "github.com/simp-lee/ginx"
    "github.com/simp-lee/jwt"
    "github.com/simp-lee/rbac"
    shardedcache "github.com/simp-lee/cache"
)

func main() {
    r := gin.New()
    
    // Setup services with proper configuration
    jwtService, _ := jwt.New("your-super-secret-key-here",
        jwt.WithLeeway(5*time.Minute),
        jwt.WithIssuer("ginx-app"),
        jwt.WithMaxTokenLifetime(24*time.Hour),
    )
    rbacService, _ := rbac.New() // Default memory storage
    cache := shardedcache.NewCache(shardedcache.Options{
        MaxSize:           1000,
        DefaultExpiration: 5 * time.Minute,
        ShardCount:        16,              // Concurrent access optimization
        CleanupInterval:   1 * time.Minute, // Automatic cleanup
    })
    
    // Production middleware chain with conditional logic
    isAPIPath := ginx.PathHasPrefix("/api/")
    isPublicPath := ginx.Or(ginx.PathIs("/api/login", "/api/register"))
    isHealthPath := ginx.Or(ginx.PathIs("/health", "/metrics"))
    isAdminPath := ginx.PathHasPrefix("/api/admin/")
    
    r.Use(ginx.NewChain().
        OnError(func(c *gin.Context, err error) {
            c.JSON(500, gin.H{"error": "Internal server error"})
        }).
        // Base middleware for all requests
        Use(ginx.Recovery()).
        Use(ginx.Logger()).
        // CORS for web clients (production origins)
        Use(ginx.CORS(
            ginx.WithAllowOrigins("https://yourdomain.com", "https://app.yourdomain.com"),
            ginx.WithAllowMethods("GET", "POST", "PUT", "DELETE", "OPTIONS"),
            ginx.WithAllowHeaders("Content-Type", "Authorization"),
            ginx.WithAllowCredentials(true),
        )).
        // Different timeouts for different endpoint types
        When(ginx.PathHasPrefix("/api/heavy/"), 
            ginx.Timeout(ginx.WithTimeout(60*time.Second))).
        Unless(isHealthPath, 
            ginx.Timeout(ginx.WithTimeout(30*time.Second))).
        // Rate limiting (skip health checks)
        When(ginx.Not(isHealthPath), 
            ginx.RateLimit(100, 200)).
        // JWT authentication for API routes (skip public endpoints)
        When(ginx.And(isAPIPath, ginx.Not(isPublicPath)),
            ginx.Auth(jwtService)).
        // Admin area protection
        When(isAdminPath, 
            ginx.RequirePermission(rbacService, "admin", "access")).
        // Cache GET API responses (authenticated users only)
        When(ginx.And(ginx.MethodIs("GET"), isAPIPath, ginx.IsAuthenticated()),
            ginx.Cache(cache)).
        Build())
        
    // Routes
    r.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok"})
    })
    
    api := r.Group("/api")
    {
        api.POST("/login", handleLogin)
        api.GET("/users", handleGetUsers)      // Cached
        api.POST("/users", handleCreateUser)   // Not cached
        
        admin := api.Group("/admin")
        {
            admin.GET("/stats", handleAdminStats)     // Requires admin role
            admin.DELETE("/users/:id", handleDeleteUser) // Requires admin role
        }
    }
    
    r.Run(":8080")
}
```

### Microservice with Conditional Rate Limiting

```go
func setupMicroservice() gin.HandlerFunc {
    return ginx.NewChain().
        Use(ginx.Recovery()).
        Use(ginx.Logger()).
        Use(ginx.Timeout(ginx.WithTimeout(10*time.Second))).
        // Different rate limits for different client types
        When(ginx.PathHasPrefix("/internal/"), 
            ginx.RateLimit(1000, 2000)).  // High limits for internal services
        When(ginx.PathHasPrefix("/api/public/"), 
            ginx.RateLimit(10, 20)).      // Low limits for public API
        When(ginx.And(
            ginx.PathHasPrefix("/api/"),
            ginx.HeaderExists("X-API-Key"),
        ), ginx.RateLimit(100, 200)).     // Medium limits for API key users
        Build()
}
```

### Multi-tenant SaaS Application

```go
// Per-tenant rate limiting with dynamic limits based on subscription plan
r.Use(ginx.RateLimit(0, 0,
    ginx.WithUser(),  // Rate limit per user
    ginx.WithDynamicLimits(func(key string) (int, int) {
        // key format: "user:<id>"
        if strings.Contains(key, "user:premium_") {
            return 1000, 2000  // Premium users: 1000 RPS, burst 2000
        }
        if strings.Contains(key, "user:pro_") {
            return 100, 200    // Pro users: 100 RPS, burst 200
        }
        return 10, 20          // Free users: 10 RPS, burst 20
    }),
))

// Feature-based conditional access control
isAnalyticsPath := ginx.PathHasPrefix("/api/analytics/")
isBillingPath := ginx.PathHasPrefix("/api/billing/")
isReportingPath := ginx.PathHasPrefix("/api/reporting/")

r.Use(ginx.NewChain().
    // Analytics requires analytics permission
    When(isAnalyticsPath, 
        ginx.RequireRolePermission(rbacService, "analytics", "read")).
    // Billing requires billing access
    When(isBillingPath, 
        ginx.RequireRolePermission(rbacService, "billing", "access")).
    // Advanced reporting for premium users only
    When(isReportingPath,
        ginx.RequireRolePermission(rbacService, "reporting", "generate")).
    // Cache expensive analytics queries
    When(ginx.And(isAnalyticsPath, ginx.MethodIs("GET")),
        ginx.CacheWithGroup(cache, "analytics")).
    Build())
```

### Complete Cache Strategy Example

```go
// Real-world caching strategy with multiple cache groups and conditions
func setupAdvancedCaching(r *gin.Engine, cache shardedcache.CacheInterface) {
    // Define path conditions for clarity
    isAPIPath := ginx.PathHasPrefix("/api/")
    isPublicData := ginx.PathHasPrefix("/public/")
    isUserSpecific := ginx.PathHasPrefix("/api/users/")
    isAdminData := ginx.PathHasPrefix("/admin/")
    
    // Advanced caching chain with different strategies
    r.Use(ginx.NewChain().
        // Cache public data aggressively (separate group for easy management)
        When(ginx.And(ginx.MethodIs("GET"), isPublicData),
            ginx.CacheWithGroup(cache, "public")).
        // Cache API GET requests but exclude health/status endpoints
        When(ginx.And(
            ginx.MethodIs("GET"),
            isAPIPath,
            ginx.Not(ginx.Or(ginx.PathIs("/api/health", "/api/status"))),
        ), ginx.CacheWithGroup(cache, "api")).
        // User-specific data with separate group (privacy isolation)
        When(ginx.And(ginx.MethodIs("GET"), isUserSpecific),
            ginx.CacheWithGroup(cache, "users")).
        // Never cache admin data (add no-cache headers)
        When(isAdminData, ginx.Custom(func(c *gin.Context) bool {
            c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
            return false // Skip caching middleware entirely
        })).
        Build())
}
```

## Performance Notes

- **Conditions efficiency**: Most conditions are zero-allocation; `ContentTypeIs` parses MIME (small overhead), and `PathMatches` compiles the regex at condition creation time (not per request).
- **Functional composition**: Minimal middleware chain overhead with conditional execution
- **Sharded caching**: Reduced lock contention for high-concurrency scenarios  
- **Token bucket precision**: Smooth rate limiting with automatic memory cleanup
- **Compiled patterns**: Cached regex for `PathMatches()` condition

## Dependencies

**Core dependencies:**
- `github.com/gin-gonic/gin` v1.10.1 - Web framework
- `golang.org/x/time` v0.13.0 - Rate limiting implementation

**Optional feature dependencies:**
- `github.com/simp-lee/jwt` - JWT authentication (for Auth middleware)
- `github.com/simp-lee/rbac` - Role-based access control (for RBAC middleware)  
- `github.com/simp-lee/logger` - Structured logging (for Logger/Recovery middleware)
- `github.com/simp-lee/cache` - Response caching (for Cache middleware)

**Testing:**
- `github.com/stretchr/testify` v1.11.1 - Test assertions

## License

MIT