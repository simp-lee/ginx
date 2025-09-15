# Ginx Rate Limiting Examples

This directory contains comprehensive examples demonstrating all features of the Ginx rate limiting middleware.

## Quick Start

```bash
# Run the example server
go run main.go

# Test basic rate limiting
curl -i http://localhost:8080/basic/global

# Test with rapid requests
for i in {1..15}; do curl -i http://localhost:8080/basic/global; done
```

## Examples Overview

### 1. Basic Rate Limiting (`/basic/*`)

- **Global Rate Limit** (`/basic/global`)
  - 10 requests per second, burst of 20
  - Shared limit across all clients

- **Per-IP Rate Limit** (`/basic/per-ip`)
  - 5 requests per second, burst of 10
  - Each IP gets separate rate limit bucket

- **Per-User Rate Limit** (`/basic/per-user`)
  - 3 requests per second, burst of 6
  - Requires `?user_id=` parameter
  - Each user gets separate rate limit

- **Per-Path Rate Limit** (`/basic/per-path`)
  - 2 requests per second, burst of 4
  - Each client+path combination gets separate limit

### 2. Advanced Configuration (`/advanced/*`)

- **Custom Key Function** (`/advanced/custom-key`)
  - Rate limiting by IP + User-Agent combination
  - 8 requests per second, burst of 15

- **Skip Function** (`/advanced/skip-admin`)
  - 4 requests per second, burst of 8
  - Admins bypass rate limiting with `?admin=true`

- **No Headers** (`/advanced/no-headers`)
  - 6 requests per second, burst of 12
  - No `X-RateLimit-*` headers in response

- **With Waiting** (`/advanced/with-wait`)
  - 3 requests per second, burst of 5
  - Requests wait up to 2 seconds for available tokens

### 3. Dynamic Rate Limiting (`/dynamic/*`)

- **Premium Users** (`/dynamic/premium`)
  - Regular users: 10 rps, 20 burst
  - Premium users: 50 rps, 100 burst
  - Use `?user_id=premium1` for premium limits

- **Tiered Limits** (`/dynamic/tiered`)
  - Enterprise users (`user_id=enterprise1`): 200 rps, 400 burst
  - Premium users (`user_id=premium1`): 50 rps, 100 burst
  - Basic users (`user_id=basic1`): 10 rps, 20 burst
  - Free users (default): 5 rps, 10 burst

### 4. Special Features (`/special/*`)

- **Conditional Rate Limiting** (`/special/condition`)
  - Demonstrates middleware chains
  - Adds warning headers

- **Metrics Endpoint** (`/special/metrics`)
  - Mock metrics display
  - Shows how to implement rate limiting monitoring

### 5. Core Conditional Architecture (`/conditional/*`) - **Ginx's Unique Features**

This section showcases Ginx's core design philosophy: **极简 + 可组合 + 条件执行** (Minimalist + Composable + Conditional Execution)

- **Smart Conditional** (`/conditional/smart`)
  - Demonstrates `When()` and `Unless()` with condition DSL
  - Path-based, method-based, and header-based conditions
  - Health check bypass example

- **API Conditional Chains** (`POST /conditional/api/*`)
  - Complex condition combinations with `And()`, `Or()`, `Not()`
  - Different limits for public vs authenticated API
  - High-volume endpoint detection

- **Complex Conditions** (`/conditional/complex`)
  - Real-world conditional scenarios
  - Content-Type based rate limiting
  - User-Agent detection with custom conditions

- **Production Setup** (`/conditional/production`)
  - Production-ready conditional patterns
  - Internal service bypass
  - API version-based limits

## Testing Rate Limits

### Using curl
```bash
# Test basic rate limiting
curl -i http://localhost:8080/basic/global

# Check rate limit headers
curl -i http://localhost:8080/basic/per-ip | grep X-RateLimit

# Test per-user limits
curl -i "http://localhost:8080/basic/per-user?user_id=testuser"

# Test premium user limits
curl -i "http://localhost:8080/dynamic/premium?user_id=premium1"

# Test admin bypass
curl -i "http://localhost:8080/advanced/skip-admin?admin=true"

# Test core conditional architecture features
curl -i http://localhost:8080/conditional/smart
curl -i http://localhost:8080/conditional/smart/health
curl -i -X POST http://localhost:8080/conditional/api/data
curl -i -X POST -H "Authorization: Bearer token" http://localhost:8080/conditional/api/bulk
curl -i -H "Content-Type: application/json" http://localhost:8080/conditional/complex
curl -i -H "X-Admin: true" http://localhost:8080/conditional/smart
curl -i -H "X-Internal-Service: true" http://localhost:8080/conditional/production
```

### Using Apache Bench
```bash
# Stress test with 100 requests, 10 concurrent
ab -n 100 -c 10 http://localhost:8080/basic/per-ip

# Test rate limiting effectiveness
ab -n 50 -c 5 -t 10 http://localhost:8080/basic/global
```

### Using a simple loop
```bash
# Bash loop to trigger rate limits
for i in {1..20}; do
  echo "Request $i:"
  curl -i http://localhost:8080/basic/global | grep -E "(HTTP|X-RateLimit)"
  sleep 0.1
done
```

## Rate Limit Headers

When rate limiting is active, responses include:
- `X-RateLimit-Limit`: Maximum requests per second
- `X-RateLimit-Remaining`: Remaining requests in current window
- `X-RateLimit-Reset`: Unix timestamp when limit resets

Example:
```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 7
X-RateLimit-Reset: 1640995200
```

## Response When Rate Limited

When rate limit is exceeded:
```json
HTTP/1.1 429 Too Many Requests
Content-Type: application/json

{
  "error": "rate limit exceeded",
  "retry_after": 0.1
}
```

## Code Patterns

### Basic Usage
```go
// Simple global rate limit
r.GET("/api", ginx.RateLimit(10, 20)(func(c *gin.Context) {
    c.JSON(200, gin.H{"message": "success"})
}))
```

### Advanced Configuration
```go
// Custom rate limiter with configuration
limiter := ginx.NewRateLimiter(5, 10).
    WithKeyFunc(func(c *gin.Context) string {
        return c.ClientIP() + ":" + c.Request.URL.Path
    }).
    WithSkipFunc(func(c *gin.Context) bool {
        return c.GetHeader("Authorization") == "Bearer admin"
    })

r.GET("/api", limiter.Middleware()(handler))
```

### Dynamic Rate Limiting
```go
// Different limits per user type
dynamicLimiter := ginx.NewDynamicRateLimiter(func(userID string) (rps, burst int) {
    if isPremiumUser(userID) {
        return 100, 200
    }
    return 10, 20
})

r.GET("/api", extractUserID(), dynamicLimiter.Middleware()(handler))
```

### **Core Conditional Architecture - Ginx's Unique Features**

#### Basic Conditional Chains
```go
// When/Unless with simple conditions
chain := ginx.NewChain().
    Use(ginx.Recovery()).
    // Only apply rate limiting for non-health endpoints
    When(ginx.Not(ginx.PathIs("/health")), ginx.RateLimit(10, 20)).
    // Skip rate limiting for admin users
    Unless(ginx.HeaderEquals("X-Admin", "true"), ginx.RateLimit(5, 10))

r.Use(chain.Build())
```

#### Complex Condition Combinations
```go
// Define reusable conditions
isAPIRequest := ginx.PathHasPrefix("/api")
isAuthenticated := ginx.HeaderExists("Authorization")
isHighVolume := ginx.Or(
    ginx.PathHasSuffix("/bulk"),
    ginx.PathHasSuffix("/batch"),
)

// Combine conditions with And/Or/Not
isProtectedAPI := ginx.And(isAPIRequest, isAuthenticated)
isPublicAPI := ginx.And(isAPIRequest, ginx.Not(isAuthenticated))

chain := ginx.NewChain().
    // Different limits for different API types
    When(isPublicAPI, ginx.RateLimit(10, 20)).
    When(isProtectedAPI, ginx.RateLimit(50, 100)).
    When(isHighVolume, ginx.RateLimit(100, 200))
```

#### Condition DSL Reference
```go
// Path conditions
ginx.PathIs("/api/v1", "/api/v2")
ginx.PathHasPrefix("/admin")
ginx.PathHasSuffix(".json")
ginx.PathMatches(`^/api/v\d+/`)

// HTTP conditions
ginx.MethodIs("POST", "PUT", "DELETE")
ginx.HeaderExists("Authorization")
ginx.HeaderEquals("Content-Type", "application/json")
ginx.ContentTypeIs("application/json", "text/xml")

// Logic combinations
ginx.And(cond1, cond2, cond3)
ginx.Or(cond1, cond2)
ginx.Not(cond)

// Custom conditions
ginx.Custom(func(c *gin.Context) bool {
    return strings.Contains(c.GetHeader("User-Agent"), "Bot")
})
```

#### Production-Ready Patterns
```go
// Real-world conditional setup
chain := ginx.NewChain().
    OnError(func(c *gin.Context, err error) {
        c.JSON(500, gin.H{"error": "server error"})
    }).
    Use(ginx.Recovery()).
    // Global base protection
    Use(ginx.RateLimit(100, 200)).
    // Stricter limits for anonymous users
    Unless(ginx.HeaderExists("Authorization"), ginx.RateLimit(10, 20)).
    // Bypass for internal services
    Unless(ginx.Or(
        ginx.HeaderEquals("X-Internal-Service", "true"),
        ginx.PathHasPrefix("/internal/"),
    ), ginx.RateLimit(5, 10)).
    // API version specific limits
    When(ginx.PathHasPrefix("/v1/"), ginx.RateLimit(50, 100)).
    When(ginx.PathHasPrefix("/v2/"), ginx.RateLimit(100, 200))

r.Use(chain.Build())
```

## Key Functions

The examples demonstrate several key generation strategies:

- **IP-based**: `c.ClientIP()` - separate limits per IP
- **User-based**: `"user:" + userID` - separate limits per user
- **Path-based**: `c.ClientIP() + ":" + c.Request.URL.Path` - per client+path
- **Custom**: `c.ClientIP() + ":" + c.GetHeader("User-Agent")` - custom logic

## Production Considerations

1. **Storage Backend**: Use Redis for distributed systems
2. **Key Design**: Choose keys that align with your rate limiting goals
3. **Monitoring**: Implement metrics collection for rate limiting effectiveness
4. **Graceful Degradation**: Handle storage failures appropriately
5. **Security**: Prevent key enumeration and abuse

## Further Reading

- See `ratelimit.go` for implementation details
- Check `ratelimit_test.go` for comprehensive test examples
- Review the main Ginx documentation for integration patterns