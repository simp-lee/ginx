package ginx

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// ============================================================================
// Rate Limiting - Simplified and High-Performance Design
// ============================================================================

// RateLimitStore 限流器存储接口 - 直接操作 rate.Limiter
type RateLimitStore interface {
	// Get returns the limiter for the given key
	Get(key string) (*rate.Limiter, bool)
	// Set stores the limiter for the given key
	Set(key string, limiter *rate.Limiter)
	// Delete removes the limiter for the given key
	Delete(key string)
	// Clear removes all expired limiters
	Clear()
	// Close cleans up resources
	Close() error
}

// MemoryLimiterStore 内存存储实现
type MemoryLimiterStore struct {
	mu         sync.RWMutex
	limiters   map[string]*rate.Limiter
	lastAccess map[string]time.Time
	maxIdle    time.Duration

	// 清理协程控制
	ticker *time.Ticker
	done   chan struct{}
}

// NewMemoryLimiterStore 创建内存存储
func NewMemoryLimiterStore(maxIdle time.Duration) *MemoryLimiterStore {
	if maxIdle <= 0 {
		maxIdle = 5 * time.Minute
	}

	store := &MemoryLimiterStore{
		limiters:   make(map[string]*rate.Limiter),
		lastAccess: make(map[string]time.Time),
		maxIdle:    maxIdle,
		done:       make(chan struct{}),
	}

	// 启动清理协程
	store.ticker = time.NewTicker(maxIdle / 2)
	go store.cleanup()

	return store
}

func (s *MemoryLimiterStore) Get(key string) (*rate.Limiter, bool) {
	s.mu.RLock()
	limiter, exists := s.limiters[key]
	if exists {
		s.lastAccess[key] = time.Now()
	}
	s.mu.RUnlock()
	return limiter, exists
}

func (s *MemoryLimiterStore) Set(key string, limiter *rate.Limiter) {
	s.mu.Lock()
	s.limiters[key] = limiter
	s.lastAccess[key] = time.Now()
	s.mu.Unlock()
}

func (s *MemoryLimiterStore) Delete(key string) {
	s.mu.Lock()
	delete(s.limiters, key)
	delete(s.lastAccess, key)
	s.mu.Unlock()
}

func (s *MemoryLimiterStore) Clear() {
	s.mu.Lock()
	s.limiters = make(map[string]*rate.Limiter)
	s.lastAccess = make(map[string]time.Time)
	s.mu.Unlock()
}

func (s *MemoryLimiterStore) Close() error {
	close(s.done)
	s.ticker.Stop()
	return nil
}

func (s *MemoryLimiterStore) cleanup() {
	for {
		select {
		case <-s.done:
			return
		case now := <-s.ticker.C:
			s.mu.Lock()
			for key, lastAccess := range s.lastAccess {
				if now.Sub(lastAccess) > s.maxIdle {
					delete(s.limiters, key)
					delete(s.lastAccess, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// ============================================================================
// Rate Limiting Middleware - Simplified
// ============================================================================

// RateLimiter 速率限制中间件配置
type RateLimiter struct {
	store    RateLimitStore
	rps      int
	burst    int
	keyFunc  func(*gin.Context) string
	skipFunc func(*gin.Context) bool
	headers  bool
}

// NewRateLimiter 创建速率限制中间件
func NewRateLimiter(rps, burst int) *RateLimiter {
	return &RateLimiter{
		store:   NewMemoryLimiterStore(0),
		rps:     rps,
		burst:   burst,
		keyFunc: defaultKeyFunc,
		headers: true,
	}
}

// WithStore 设置自定义存储
func (rl *RateLimiter) WithStore(store RateLimitStore) *RateLimiter {
	rl.store = store
	return rl
}

// WithKeyFunc 设置键生成函数
func (rl *RateLimiter) WithKeyFunc(keyFunc func(*gin.Context) string) *RateLimiter {
	rl.keyFunc = keyFunc
	return rl
}

// WithSkipFunc 设置跳过函数
func (rl *RateLimiter) WithSkipFunc(skipFunc func(*gin.Context) bool) *RateLimiter {
	rl.skipFunc = skipFunc
	return rl
}

// WithoutHeaders 禁用HTTP头
func (rl *RateLimiter) WithoutHeaders() *RateLimiter {
	rl.headers = false
	return rl
}

// Middleware 构建中间件
func (rl *RateLimiter) Middleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if rl.skipFunc != nil && rl.skipFunc(c) {
				next(c)
				return
			}

			key := rl.keyFunc(c)
			limiter := rl.getLimiter(key)

			if !limiter.Allow() {
				rl.handleRateLimit(c, limiter)
				return
			}

			if rl.headers {
				rl.setHeaders(c, limiter)
			}

			next(c)
		}
	}
}

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	limiter, exists := rl.store.Get(key)
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(rl.rps), rl.burst)
		rl.store.Set(key, limiter)
	}
	return limiter
}

func (rl *RateLimiter) handleRateLimit(c *gin.Context, limiter *rate.Limiter) {
	// 使用 Reserve 获取准确的等待时间
	reservation := limiter.Reserve()
	if !reservation.OK() {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error": "rate limit exceeded",
		})
		return
	}

	delay := reservation.Delay()
	reservation.Cancel() // 取消预约

	if rl.headers {
		c.Header("Retry-After", strconv.FormatInt(int64(delay.Seconds())+1, 10))
	}

	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":       "rate limit exceeded",
		"retry_after": int64(delay.Seconds()) + 1,
	})
}

// SetHeaders 设置速率限制头 (待核实)
func (rl *RateLimiter) setHeaders(c *gin.Context, limiter *rate.Limiter) {
	c.Header("X-RateLimit-Limit", strconv.Itoa(rl.rps))
	remaining := int(limiter.Tokens())
	c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
	reset := time.Now().Add(time.Duration(float64(rl.burst)/float64(rl.rps)) * time.Second)
	c.Header("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
}

// Close 关闭限流器
func (rl *RateLimiter) Close() error {
	return rl.store.Close()
}

// ============================================================================
// Convenience Functions - Super Simple API
// ============================================================================

// RateLimit 便捷函数：创建速率限制中间件
func RateLimit(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).Middleware()
}

// RateLimitByIP 基于IP的速率限制
func RateLimitByIP(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).
		WithKeyFunc(KeyByIP()).
		Middleware()
}

// RateLimitByUser 基于用户的速率限制
func RateLimitByUser(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).
		WithKeyFunc(KeyByUserID()).
		Middleware()
}

// RateLimitByPath 基于路径的速率限制
func RateLimitByPath(rps, burst int) Middleware {
	return NewRateLimiter(rps, burst).
		WithKeyFunc(KeyByPath()).
		Middleware()
}

// ============================================================================
// Advanced Features - With Wait Support
// ============================================================================

// RateLimitWithWait 支持等待的速率限制中间件
func RateLimitWithWait(rps, burst int, timeout time.Duration) Middleware {
	limiter := NewRateLimiter(rps, burst)

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if limiter.skipFunc != nil && limiter.skipFunc(c) {
				next(c)
				return
			}

			key := limiter.keyFunc(c)
			rateLimiter := limiter.getLimiter(key)

			// 使用 Wait 方法等待
			ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
			defer cancel()

			if err := rateLimiter.Wait(ctx); err != nil {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":   "rate limit exceeded",
					"timeout": timeout.Seconds(),
				})
				return
			}

			next(c)
		}
	}
}

// ============================================================================
// Conditions - Rate Limiting
// ============================================================================

// IsRateLimited 检查是否被限流（不消费token）
func IsRateLimited(store RateLimitStore, rps, burst int, keyFunc func(*gin.Context) string) Condition {
	if keyFunc == nil {
		keyFunc = defaultKeyFunc
	}

	return func(c *gin.Context) bool {
		key := keyFunc(c)
		limiter, exists := store.Get(key)
		if !exists {
			// 不存在说明还没有请求过，不会被限流
			return false
		}

		// 使用 Reserve 检查但不消费token
		reservation := limiter.Reserve()
		if !reservation.OK() {
			return true
		}

		delay := reservation.Delay()
		reservation.Cancel() // 取消预约

		return delay > 0
	}
}

// ============================================================================
// Per-User Dynamic Rate Limiting
// ============================================================================

// DynamicRateLimiter 动态速率限制器
type DynamicRateLimiter struct {
	store      RateLimitStore
	getLimiter func(key string) (rps int, burst int)
	keyFunc    func(*gin.Context) string
	skipFunc   func(*gin.Context) bool
	headers    bool
}

// NewDynamicRateLimiter 创建动态速率限制器
func NewDynamicRateLimiter(getLimiter func(key string) (rps int, burst int)) *DynamicRateLimiter {
	return &DynamicRateLimiter{
		store:      NewMemoryLimiterStore(0),
		getLimiter: getLimiter,
		keyFunc:    KeyByUserID(),
		headers:    true,
	}
}

// Middleware 构建动态中间件
func (dl *DynamicRateLimiter) Middleware() Middleware {
	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if dl.skipFunc != nil && dl.skipFunc(c) {
				next(c)
				return
			}

			key := dl.keyFunc(c)
			limiter := dl.getRateLimiter(key)

			if !limiter.Allow() {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "rate limit exceeded",
				})
				return
			}

			next(c)
		}
	}
}

func (dl *DynamicRateLimiter) getRateLimiter(key string) *rate.Limiter {
	limiter, exists := dl.store.Get(key)
	if !exists {
		rps, burst := dl.getLimiter(key)
		limiter = rate.NewLimiter(rate.Limit(rps), burst)
		dl.store.Set(key, limiter)
	}
	return limiter
}

// RateLimitPerUser 每用户动态限制的便捷函数
func RateLimitPerUser(getLimiter func(userID string) (rps int, burst int)) Middleware {
	return NewDynamicRateLimiter(getLimiter).Middleware()
}

// ============================================================================
// Helper Functions
// ============================================================================

func defaultKeyFunc(c *gin.Context) string {
	return c.ClientIP()
}

// KeyByIP 基于IP的键生成器
func KeyByIP() func(*gin.Context) string {
	return func(c *gin.Context) string {
		return c.ClientIP()
	}
}

// KeyByUserID 基于用户ID的键生成器
func KeyByUserID() func(*gin.Context) string {
	return func(c *gin.Context) string {
		if userID, exists := c.Get("user_id"); exists {
			if id, ok := userID.(string); ok {
				return "user:" + id
			}
		}
		return c.ClientIP() // 回退到IP
	}
}

// KeyByPath 基于路径的键生成器
func KeyByPath() func(*gin.Context) string {
	return func(c *gin.Context) string {
		return fmt.Sprintf("%s:%s", c.ClientIP(), c.Request.URL.Path)
	}
}
