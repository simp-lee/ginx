# Combined Rate Limiting Example

This example demonstrates how to **simultaneously use** the original RPS rate limiting and the new time-window rate limiting.

## Core Concepts

### ✅ Yes, You Can Use Them Together!

**Yes, the new time-window rate limiting can be used together with the original per-second (RPS) rate limiting!**

Use `ginx.NewChain()` to combine multiple rate limiting middlewares:

```go
router.Use(ginx.NewChain().
    Use(ginx.RateLimit(10, 20)).        // Per-second rate limiting
    Use(ginx.RateLimitPerMinute(100)).  // Per-minute rate limiting
    Use(ginx.RateLimitPerHour(1000)).   // Per-hour rate limiting
    Build())
```

## Rate Limiting Types Comparison

### 1. RPS Rate Limiting (Original Feature)
- **Function**: `ginx.RateLimit(rps, burst)`
- **Algorithm**: Token Bucket
- **Advantages**: Allows short bursts, smooth traffic flow
- **Use Cases**: Prevent instant traffic spikes

### 2. Time-Window Rate Limiting (New Feature)
- **Function**: `ginx.RateLimitPerMinute/Hour/Day(limit)`
- **Algorithm**: Fixed Window Counter
- **Advantages**: Precise quota management, strict limits within windows
- **Use Cases**: API quotas, prevent sustained abuse
- **Note**: May experience burst traffic at window boundaries

### 3. Combined Use (Recommended)
- **Multi-Layer Protection**: RPS + Time Window
- **Advantages**: 
  - RPS handles instant peaks
  - Window limiting controls total quota
  - Flexible response to different attack patterns

## Example Descriptions

### Example 1: Two-Layer Protection
```go
example1.Use(ginx.NewChain().
    Use(ginx.RateLimit(10, 20)).       // Max 10 rps (burst of 20)
    Use(ginx.RateLimitPerMinute(100)). // Max 100 per minute
    Build())
```

**Effect**:
- Normal case: 10 requests per second pass through
- Burst case: Allows instant 20 requests
- Quota control: No more than 100 requests per minute regardless

### Example 2: Three-Layer Protection
```go
example2.Use(ginx.NewChain().
    Use(ginx.RateLimit(5, 10)).        // Instant protection
    Use(ginx.RateLimitPerHour(1000)).  // Hourly quota
    Use(ginx.RateLimitPerDay(10000)).  // Daily quota
    Build())
```

**Protection Layers**:
1. **First Layer (RPS)**: Prevent instant DDoS attacks
2. **Second Layer (Hourly)**: Prevent sustained abuse
3. **Third Layer (Daily)**: Total quota management

### Example 3: User Tier-Based Rate Limiting
```go
// Free tier users
freeGroup.Use(ginx.NewChain().
    Use(ginx.RateLimit(1, 2, ginx.WithUser())).
    Use(ginx.RateLimitPerHour(50, ginx.WithUser())).
    Build())

// Premium tier users
premiumGroup.Use(ginx.NewChain().
    Use(ginx.RateLimit(50, 100, ginx.WithUser())).
    Use(ginx.RateLimitPerHour(5000, ginx.WithUser())).
    Build())
```

**User Differentiation**: Use `WithUser()` option to apply different rate limiting strategies for different users.

### Example 4: Endpoint-Specific Rate Limiting
```go
// Lightweight endpoint: High RPS + Moderate daily quota
router.GET("/ping", 
    ginx.NewChain().
        Use(ginx.RateLimit(100, 200)).
        Use(ginx.RateLimitPerDay(100000)).
        Build(),
    handler)

// Heavy endpoint: Low RPS + Low hourly quota
router.POST("/heavy", 
    ginx.NewChain().
        Use(ginx.RateLimit(1, 2)).
        Use(ginx.RateLimitPerHour(10)).
        Build(),
    handler)
```

## Running the Example

### Start the Server
```bash
cd examples/ratelimit_combined
go run main.go
```

### Test Two-Layer Rate Limiting
```bash
# Test Example 1 (10 rps + 100/min)
curl http://localhost:8080/api/example1/data

# Send multiple requests quickly to test RPS limiting
for i in {1..30}; do curl http://localhost:8080/api/example1/data & done
```

### Test Three-Layer Rate Limiting
```bash
# Test Example 2 (5 rps + 1000/hour + 10000/day)
curl http://localhost:8080/api/example2/premium
```

### Test Different API Versions
```bash
# V1: RPS rate limiting only
curl http://localhost:8080/v1/users

# V2: Time-window rate limiting only
curl http://localhost:8080/v2/users

# V3: Combined rate limiting
curl http://localhost:8080/v3/users
```

## Response Headers Explained

When using multiple rate limiters simultaneously, each sets its own response headers:

### RPS Rate Limiting Headers
```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 9
X-RateLimit-Reset: 1708156801
```

`X-RateLimit-Reset` is a Unix timestamp in seconds (unit: Unix seconds timestamp).

### Time-Window Rate Limiting Headers
```
X-RateLimit-Limit-Minute: 100
X-RateLimit-Remaining-Minute: 95
X-RateLimit-Reset-Minute: 45
```

### When Rate Limited
```
HTTP/1.1 429 Too Many Requests
Retry-After: 30
X-RateLimit-Limit-Hour: 1000
X-RateLimit-Remaining-Hour: 0
X-RateLimit-Reset-Hour: 1800
```

## Best Practices

### 1. Choose Appropriate Combined Strategies

#### Public APIs
```go
Use(ginx.RateLimit(10, 20)).        // Moderate RPS
Use(ginx.RateLimitPerDay(10000)).   // Daily quota
```

#### Internal APIs
```go
Use(ginx.RateLimit(100, 200)).      // High RPS
Use(ginx.RateLimitPerHour(50000)).  // Generous quota
```

#### Sensitive Operations
```go
Use(ginx.RateLimit(1, 1)).          // Strict RPS
Use(ginx.RateLimitPerHour(10)).     // Low quota
Use(ginx.RateLimitPerDay(50)).      // Total quota control
```

### 2. Configuration Recommendations

| Scenario | RPS | Burst | Minute | Hour | Day |
|----------|-----|-------|--------|------|-----|
| Read Operations | 50 | 100 | 500 | 5000 | 50000 |
| Write Operations | 10 | 20 | 100 | 1000 | 10000 |
| Search Operations | 5 | 10 | 50 | 500 | 5000 |
| Login Operations | 1 | 2 | 5 | 20 | 100 |

### 3. User Tier Configuration Example

```go
type RateLimitConfig struct {
    RPS     int
    Burst   int
    PerHour int
    PerDay  int
}

var configs = map[string]RateLimitConfig{
    "free": {
        RPS:     1,
        Burst:   2,
        PerHour: 50,
        PerDay:  500,
    },
    "basic": {
        RPS:     10,
        Burst:   20,
        PerHour: 1000,
        PerDay:  10000,
    },
    "premium": {
        RPS:     50,
        Burst:   100,
        PerHour: 5000,
        PerDay:  100000,
    },
}

// Usage:
cfg := configs["premium"]
router.Use(ginx.NewChain().
    Use(ginx.RateLimit(cfg.RPS, cfg.Burst, ginx.WithUser())).
    Use(ginx.RateLimitPerHour(cfg.PerHour, ginx.WithUser())).
    Use(ginx.RateLimitPerDay(cfg.PerDay, ginx.WithUser())).
    Build())
```

## How It Works

### Execution Order
When a request arrives, middlewares execute in the order specified by `Use()`:

```go
ginx.NewChain().
    Use(Limiter1).  // Check first
    Use(Limiter2).  // Check second
    Use(Limiter3).  // Check last
    Build()
```

**If any limiter rejects the request, it immediately returns a 429 error**.

### Independent Counters
Each rate limiter maintains its own counter:
- RPS rate limiting uses Token Bucket algorithm
- Time-window rate limiting uses Fixed Window Counter algorithm
- They don't interfere with each other and work independently

### Window Reset Times
Fixed windows reset at the following time points:
- **Minute**: At 0 seconds of each minute (e.g., 14:35:00)
- **Hour**: At 0 minutes 0 seconds of each hour (e.g., 14:00:00)
- **Day**: At 00:00:00 each day

## Performance Considerations

### Memory Usage
- **RPS Rate Limiting**: ~200 bytes per key (token bucket state)
- **Window Rate Limiting**: ~100 bytes per key (counter)
- **Combined Use**: Memory usage adds up

### Performance Recommendations
1. Set reasonable cleanup intervals (default 5 minutes)
2. Use `WithSkipFunc()` to skip requests that don't need rate limiting
3. Consider using Redis storage for high-traffic endpoints (custom Store)

## FAQ

### Q1: How do multiple rate limiters work together?
A: Each rate limiter checks independently. **If any one exceeds the limit, it returns 429**. They have an "AND" relationship.

### Q2: How many rate limiters should I use?
A: We recommend 2-3. For example:
- 2 limiters: RPS + Daily quota
- 3 limiters: RPS + Hourly + Daily

### Q3: Does the order of rate limiters matter?
A: From a performance perspective, it's recommended to place the **strictest one first** so requests can be rejected earlier.

### Q4: How do I debug rate limiting issues?
A: Check the `X-RateLimit-*` fields in response headers to see each limiter's status.

## Summary

✅ **The new time-window rate limiting works perfectly with the original RPS rate limiting**

🎯 **Advantages of Combined Use**:
- Multi-layer protection is more secure
- Flexible response to different scenarios
- Fine-grained traffic control

💡 **Recommended Strategy**:
- Use RPS rate limiting to handle burst traffic
- Use time-window rate limiting to manage quotas
- Combine flexibly based on business needs
