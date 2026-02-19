# Ginx - Functional Middleware for Gin

Minimal, composable, and high-performance middleware toolkit for Gin, with conditional execution and functional chaining.

## Features

- Functional composition: Chain + Condition to precisely control execution
- Production-ready: recovery, logging, timeout, CORS, auth, RBAC, cache, rate limit
- High performance: zero-allocation conditions, token-bucket & time-window rate limiting, sharded cache
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

    // Basic middleware stack (recommended order)
    r.Use(ginx.NewChain().
        Use(ginx.RequestID()).                // Correlation id first
        Use(ginx.Recovery()).                 // Panic protection with logging
        Use(ginx.Logger()).                   // Structured request logging
        Use(ginx.Timeout()).                  // 30s timeout protection
        Use(ginx.CORS(ginx.WithAllowOrigins("*"))). // CORS for development
        Use(ginx.RateLimit(100, 200)).        // 100 RPS, 200 burst per IP
        Build())

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
    Use(ginx.RequestID()).
    Use(ginx.Recovery()).
    Use(ginx.Logger()).
    // Apply rate limiting only to API routes
    When(ginx.PathHasPrefix("/api/"), ginx.RateLimit(100, 200)).
    // Add hourly quota for API routes
    When(ginx.PathHasPrefix("/api/"), ginx.RateLimitPerHour(10000)).
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
- `HasRequestID()` - Request has a request ID set in context

**RBAC conditions (require auth):**
- `IsAuthenticated()` - User is authenticated
- `HasPermission(service rbac.Service, resource, action string)` - Combined role + user permissions
- `HasRolePermission(service rbac.Service, resource, action string)` - Role-based permissions only
- `HasUserPermission(service rbac.Service, resource, action string)` - Direct user permissions only

## Middleware Overview

### RequestID (correlation id)

Lightweight request correlation ID middleware. It sets/propagates a unique ID via header (default: `X-Request-ID`), stores it in Gin context, and can optionally inject the ID into Go's standard `context.Context`.

**Usage:**
- `RequestID(options...)` - Adds/propagates request id

**Options:**
- `WithRequestIDHeader(name)` - Change header name (default: `X-Request-ID`)
- `WithRequestIDGenerator(func() string)` - Custom ID generator
- `WithContextInjector(func(ctx context.Context, requestID string) context.Context)` - Inject request metadata into Go context (for service/repository logging with `slog.InfoContext` etc.)
- Default respects incoming header if present; use `WithIgnoreIncoming()` to always generate a new ID

**Type:**
```go
type ContextInjector func(ctx context.Context, requestID string) context.Context
```

**Context helpers:**
- `SetRequestID(c, id string)` - Set request ID in context
- `GetRequestID(c) (string, bool)` - Get request ID from context

**Utility:**
- `GetRequestIDFromHeader(r *http.Request, header string) string` - Extract request ID from HTTP request header

**Condition:**
- `HasRequestID()` - Check if request has a request ID set in context

**Notes:**
- Logging and Recovery middlewares automatically include `request_id` if present
- Place RequestID early in the chain (before Logger/Recovery) so all logs include the id
- The middleware also echoes the ID back in the response header

**Example: inject into Go context (for context-aware slog):**
```go
package main

import (
    "context"
    "log/slog"

    "github.com/gin-gonic/gin"
    "github.com/simp-lee/ginx"
    "github.com/simp-lee/logger"
)

func main() {
    r := gin.New()

    r.Use(ginx.RequestID(
        ginx.WithContextInjector(func(ctx context.Context, id string) context.Context {
            return logger.WithContextAttrs(ctx, slog.String("request_id", id))
        }),
    ))

    r.GET("/users", func(c *gin.Context) {
        // Service/repository layers can now read request_id from c.Request.Context()
        // and emit it automatically with slog.InfoContext.
        slog.InfoContext(c.Request.Context(), "list users")
        c.JSON(200, gin.H{"ok": true})
    })
}
```

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
- `WithMaxBufferSize(size int)` - Set maximum response buffer size in bytes (default: 0 = unlimited)

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

JWT authentication middleware with secure-by-default token extraction and comprehensive context integration.

**Usage:**
- `Auth(jwtService jwt.Service, options ...Option[AuthConfig])` - JWT authentication middleware
- `WithAuthQueryToken(true)` - Explicitly enable `?token=` query fallback (disabled by default)

**Features:**
- **Secure default token extraction**: Uses `Authorization: Bearer <token>` by default
- **Optional query fallback**: `?token=<token>` is only enabled with `WithAuthQueryToken(true)`
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
- `CacheWithOptions(cache shardedcache.CacheInterface, opts ...CacheOption)` - Cache with custom options (default group)
- `CacheWithGroupOptions(cache shardedcache.CacheInterface, groupName string, opts ...CacheOption)` - Grouped cache with custom options

**Features:**
- **HTTP-compliant caching**: Respects `Cache-Control: no-store/private` directives
- **Smart exclusions**: Automatically excludes responses with `Set-Cookie` headers to prevent user data leakage
- **Auth-safe default**: Skips caching when request contains `Authorization` header
- **2xx-only caching**: Only caches successful responses (200-299 status codes)
- **Safer default cache keys**: Generated from HTTP method + host + path + query (`METHOD|HOST|PATH?QUERY`)
- **Content negotiation safety**: Default keys include `Accept-Encoding` variant when present to avoid representation mix-ups
- **Configurable key strategy**: `WithCacheKeyFunc(func(*gin.Context) string)` supports custom variant/context dimensions
- **Configurable vary dimensions**: `WithCacheVaryHeaders(headers...)` extends/overrides header dimensions used by default key generation
- **Response reconstruction**: Preserves status code/body and replays full response headers including multi-value headers (`Link`, `Vary`, etc.); responses with `Set-Cookie` are not cached
- **Group isolation**: Optional grouping for cache namespace separation

**Cache key format:**
```
GET|api.example.com|/api/users                    // No query parameters
POST|api.example.com|/api/search?q=test&limit=10  // With query parameters
GET|api.example.com|/api/users|h:Accept-Encoding=gzip // Content-encoding variant
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

### Rate Limit (token bucket & time windows)

High-performance rate limiting middleware supporting both token bucket (RPS) and time-window strategies (per minute/hour/day).

#### Token Bucket Rate Limiting (RPS)

Smooth rate limiting using token bucket algorithm for requests per second.

**Usage:**
- `RateLimit(rps int, burst int, opts ...RateOption)` - Token bucket rate limiting with configurable options

**Key generation options:**
- `WithIP()` - IP-based rate limiting (default behavior)
- `WithUser()` - Per-user rate limiting using authenticated context (`user_id`)
- `WithTrustedUserHeader(name)` - Optional trusted header fallback for gateway-injected identity
- `WithPath()` - Per-path rate limiting (different limits per endpoint)
- `WithKeyFunc(keyFunc func(*gin.Context) string)` - Custom key generation function

Security note:
- `WithUser()` does not trust client-provided headers.
- Use `WithTrustedUserHeader(name)` only when the header is set by trusted infrastructure (API gateway/auth proxy) and cannot be spoofed by clients.

**Control options:**
- `WithSkipFunc(skipFunc func(*gin.Context) bool)` - Skip certain requests
- `WithWait(timeout time.Duration)` - Wait for tokens instead of immediate rejection
- `WithDynamicLimits(getLimits func(key string) (rps, burst int))` - Dynamic per-key limits
- `WithStore(store RateLimitStore)` - Custom storage backend (default: shared memory)
- `WithRateLimitResponse(response any)` - Custom 429 response body (default: JSON with error and retry_after). Follows the same pattern as `WithTimeoutResponse`.

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

#### Time-Window Rate Limiting (Per Minute/Hour/Day)

Fixed window rate limiting for precise quota management.

**Usage:**
- `RateLimitPerMinute(limit int, opts ...RateOption)` - Maximum requests per minute
- `RateLimitPerHour(limit int, opts ...RateOption)` - Maximum requests per hour
- `RateLimitPerDay(limit int, opts ...RateOption)` - Maximum requests per day

**Supported options:**
- `WithIP()` - IP-based limiting (default)
- `WithUser()` - Per-user limiting
- `WithPath()` - Per-path limiting
- `WithKeyFunc()` - Custom key function
- `WithSkipFunc()` - Skip certain requests
- `WithWindowStore(store WindowCounterStore)` - Custom storage backend
- `WithDynamicWindowLimits(getLimit func(key string) int)` - Dynamic per-key limits
- `WithoutRateLimitHeaders()` - Disable headers
- `WithoutRetryAfterHeader()` - Disable Retry-After header
- `WithRateLimitResponse(response any)` - Custom 429 response body (default: JSON with error and retry_after)

**Note:** Time-window rate limiting does not support `WithWait()` option.

**Features:**
- **Fixed window algorithm**: Precise quota control within time windows
- **Window reset times**: 
  - Minute: At 0 seconds of each minute (e.g., 14:35:00)
  - Hour: At 0 minutes of each hour (e.g., 14:00:00)
  - Day: At midnight each day (00:00:00)
- **Independent counters**: Each window maintains its own counter
- **Automatic cleanup**: Expired counters are automatically removed
- **Thread-safe**: Designed for high-concurrency environments

**HTTP headers:**
```
X-RateLimit-Limit-Minute: 60         // Maximum per minute
X-RateLimit-Remaining-Minute: 45     // Remaining this minute
X-RateLimit-Reset-Minute: 1234567890 // Window reset time (Unix timestamp)
Retry-After: 15                      // Seconds until window resets (429 only)
```

**Example:**
```go
// Limit to 60 requests per minute
r.Use(ginx.RateLimitPerMinute(60))

// Limit to 1000 requests per hour per user
r.Use(ginx.RateLimitPerHour(1000, ginx.WithUser()))

// Limit to 10000 requests per day
r.Use(ginx.RateLimitPerDay(10000))

// Dynamic per-user limits based on user tier
r.Use(ginx.RateLimitPerHour(0, // Base limit ignored when using dynamic limits
    ginx.WithUser(),
    ginx.WithDynamicWindowLimits(func(key string) int {
        if strings.Contains(key, "user:premium_") {
            return 100000  // Premium: 100k per hour
        }
        if strings.Contains(key, "user:pro_") {
            return 10000   // Pro: 10k per hour
        }
        return 1000        // Free: 1k per hour
    }),
))
```

#### Combined Rate Limiting (Recommended)

Combine RPS and time-window rate limiting for multi-layer protection.

**Usage:**
```go
// Two-layer protection: RPS + hourly quota
r.Use(ginx.NewChain().
    Use(ginx.RateLimit(10, 20)).       // Prevent instant spikes
    Use(ginx.RateLimitPerHour(1000)).  // Hourly quota management
    Build())

// Three-layer protection: RPS + hourly + daily quota
r.Use(ginx.NewChain().
    Use(ginx.RateLimit(5, 10)).        // Instant protection
    Use(ginx.RateLimitPerHour(1000)).  // Hourly quota
    Use(ginx.RateLimitPerDay(10000)).  // Daily quota
    Build())
```

**Use cases:**
- **Public APIs**: Moderate RPS + daily quota
- **Premium users**: High RPS + generous hourly/daily quota
- **Sensitive operations**: Strict RPS + low hourly/daily quota
- **Heavy endpoints**: Low RPS + low hourly quota

**Response headers** when combined:
```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 9
X-RateLimit-Reset: 1700000001      // Unix timestamp when the bucket refills
X-RateLimit-Limit-Hour: 1000
X-RateLimit-Remaining-Hour: 850
X-RateLimit-Reset-Hour: 1700003600 // Unix timestamp when the hourly window resets
Retry-After: 30
```

**Resource management:**
- Built-in shared memory stores with automatic cleanup
- Call `ginx.CleanupRateLimiters()` on application shutdown for comprehensive cleanup

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
        // Multi-layer rate limiting (skip health checks)
        When(ginx.Not(isHealthPath), 
            ginx.RateLimit(100, 200)).
        When(ginx.Not(isHealthPath), 
            ginx.RateLimitPerHour(10000)).
        When(ginx.Not(isHealthPath), 
            ginx.RateLimitPerDay(100000)).
        // JWT authentication for API routes (skip public endpoints)
        When(ginx.And(isAPIPath, ginx.Not(isPublicPath)),
            ginx.Auth(jwtService)).
        // Admin area protection
        When(isAdminPath, 
            ginx.RequirePermission(rbacService, "admin", "access")).
        // Cache only public GET API responses (requests with Authorization are skipped by default)
        When(ginx.And(ginx.MethodIs("GET"), isAPIPath, ginx.Not(ginx.IsAuthenticated())),
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
            ginx.RateLimit(10, 20)).      // Low RPS for public API
        When(ginx.PathHasPrefix("/api/public/"), 
            ginx.RateLimitPerHour(1000)). // Hourly quota for public API
        When(ginx.And(
            ginx.PathHasPrefix("/api/"),
            ginx.HeaderExists("X-API-Key"),
        ), ginx.RateLimit(100, 200)).     // Medium limits for API key users
        Build()
}
```

### Multi-tenant SaaS Application

```go
// Per-tenant RPS rate limiting with dynamic limits based on subscription plan
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

// Per-user hourly quotas based on subscription plan
r.Use(ginx.RateLimitPerHour(0,
    ginx.WithUser(),
    ginx.WithDynamicWindowLimits(func(key string) int {
        if strings.Contains(key, "user:premium_") {
            return 100000  // Premium: 100k per hour
        }
        if strings.Contains(key, "user:pro_") {
            return 10000   // Pro: 10k per hour
        }
        return 1000        // Free: 1k per hour
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
        // Never cache admin data (add no-cache headers via custom middleware)
        When(isAdminData, func(next gin.HandlerFunc) gin.HandlerFunc {
            return func(c *gin.Context) {
                c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
                next(c)
            }
        }).
        Build())
}
```

### Combined Rate Limiting Strategies

```go
// Multi-layer rate limiting for different scenarios
func setupRateLimiting(r *gin.Engine) {
    // Example 1: Public API with burst + quota protection
    publicAPI := r.Group("/api/public")
    publicAPI.Use(ginx.NewChain().
        Use(ginx.RateLimit(10, 20)).        // Prevent instant spikes
        Use(ginx.RateLimitPerHour(1000)).   // Hourly quota
        Use(ginx.RateLimitPerDay(10000)).   // Daily quota
        Build())

    // Example 2: Authenticated API with per-user limits
    authAPI := r.Group("/api/v1")
    authAPI.Use(ginx.NewChain().
        Use(ginx.RateLimit(50, 100, ginx.WithUser())).     // Per-user RPS
        Use(ginx.RateLimitPerHour(5000, ginx.WithUser())). // Per-user hourly quota
        Build())

    // Example 3: Heavy operations with strict limits
    r.POST("/api/heavy-task", 
        ginx.NewChain().
            Use(ginx.RateLimit(1, 1)).       // Only 1 request per second
            Use(ginx.RateLimitPerHour(10)).  // Max 10 per hour
            Use(ginx.RateLimitPerDay(50)).   // Max 50 per day
            Build(),
        handleHeavyTask)

    // Example 4: Path-based rate limiting for different endpoints
    r.Use(ginx.NewChain().
        When(ginx.PathHasPrefix("/api/search/"), ginx.NewChain().
            Use(ginx.RateLimit(5, 10, ginx.WithPath())).
            Use(ginx.RateLimitPerMinute(50, ginx.WithPath())).
            Build()).
        When(ginx.PathHasPrefix("/api/login"), ginx.NewChain().
            Use(ginx.RateLimit(1, 2, ginx.WithIP())).
            Use(ginx.RateLimitPerHour(5, ginx.WithIP())).
            Build()).
        Build())

    // Example 5: Dynamic per-user tier-based rate limiting
    r.Use(ginx.NewChain().
        // RPS based on user tier (supports dynamic limits)
        Use(ginx.RateLimit(0, 0,
            ginx.WithUser(),
            ginx.WithDynamicLimits(getUserRPSLimits))).
        // Hourly quota based on user tier
        Use(ginx.RateLimitPerHour(0,
            ginx.WithUser(),
            ginx.WithDynamicWindowLimits(getUserHourlyLimits))).
        // Daily quota based on user tier
        Use(ginx.RateLimitPerDay(0,
            ginx.WithUser(),
            ginx.WithDynamicWindowLimits(getUserDailyLimits))).
        Build())
}

func getUserRPSLimits(key string) (int, int) {
    // Extract user tier from key
    if strings.Contains(key, "user:premium_") {
        return 100, 200  // Premium: 100 RPS, burst 200
    }
    if strings.Contains(key, "user:pro_") {
        return 50, 100   // Pro: 50 RPS, burst 100
    }
    return 10, 20        // Free: 10 RPS, burst 20
}

func getUserHourlyLimits(key string) int {
    if strings.Contains(key, "user:premium_") {
        return 50000  // Premium: 50k per hour
    }
    if strings.Contains(key, "user:pro_") {
        return 5000   // Pro: 5k per hour
    }
    return 500        // Free: 500 per hour
}

func getUserDailyLimits(key string) int {
    if strings.Contains(key, "user:premium_") {
        return 1000000  // Premium: 1M per day
    }
    if strings.Contains(key, "user:pro_") {
        return 100000   // Pro: 100k per day
    }
    return 10000        // Free: 10k per day
}
```

## Test Helpers

Ginx exports lightweight test utilities (in `test_helpers.go`) so downstream packages can unit-test their middleware and handlers without boilerplate.

**Context & handler helpers:**
- `TestContext(method, path string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder)` - Create a ready-to-use `gin.Context` in test mode with custom method, path, headers, and a valid `RemoteAddr`
- `TestMiddleware(name string, executed *[]string) Middleware` - Create a middleware that records its execution by appending `name` to the slice
- `TestHandler(executed *[]string) gin.HandlerFunc` - Create a handler that records execution by appending `"handler"` to the slice

**Rate limit helpers:**
- `SetupRateLimitTest(t testing.TB)` - Register automatic cleanup of global rate limiter state via `t.Cleanup`; call at the start of any test that exercises rate limiting

**Assertion helpers:**
- `AssertContains(slice []string, item string) bool` - Check if a string slice contains an element
- `AssertEqual(expected, actual interface{}) bool` - Check equality of two values
- `AssertSliceEqual(expected, actual []string) bool` - Check equality of two string slices

**Example:**
```go
package myapp

import (
    "testing"
    "github.com/simp-lee/ginx"
)

func TestMyMiddleware(t *testing.T) {
    ginx.SetupRateLimitTest(t) // automatic rate-limiter cleanup

    var executed []string
    c, w := ginx.TestContext("GET", "/api/test", map[string]string{
        "Authorization": "Bearer token123",
    })

    // Build a chain: your middleware + recording handler
    chain := ginx.NewChain().
        Use(ginx.TestMiddleware("before", &executed)).
        Use(MyCustomMiddleware()).
        Build()

    chain(c)

    if w.Code != 200 {
        t.Errorf("expected 200, got %d", w.Code)
    }
    if !ginx.AssertContains(executed, "before") {
        t.Error("expected 'before' middleware to execute")
    }
}
```

## Performance Notes

- **Conditions efficiency**: Most conditions are zero-allocation; `ContentTypeIs` parses MIME (small overhead), and `PathMatches` compiles the regex at condition creation time (not per request).
- **Functional composition**: Minimal middleware chain overhead with conditional execution
- **Sharded caching**: Reduced lock contention for high-concurrency scenarios  
- **Rate limiting**: Token bucket for smooth RPS control, fixed window counters for quota management, both with automatic memory cleanup
- **Compiled patterns**: Cached regex for `PathMatches()` condition

## Dependencies

**Core dependencies:**
- `github.com/gin-gonic/gin` v1.11.0 - Web framework
- `golang.org/x/time` v0.14.0 - Rate limiting implementation

**Optional feature dependencies:**
- `github.com/simp-lee/jwt` - JWT authentication (for Auth middleware)
- `github.com/simp-lee/rbac` - Role-based access control (for RBAC middleware)  
- `github.com/simp-lee/logger` - Structured logging (for Logger/Recovery middleware)
- `github.com/simp-lee/cache` - Response caching (for Cache middleware)

**Testing:**
- `github.com/stretchr/testify` v1.11.1 - Test assertions

## License

MIT