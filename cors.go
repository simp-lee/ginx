package ginx

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORSConfig CORS 中间件配置
type CORSConfig struct {
	AllowOrigins     []string      // 允许的源，默认只允许同源
	AllowMethods     []string      // 允许的方法，默认 GET、POST、PUT、DELETE、OPTIONS
	AllowHeaders     []string      // 允许的请求头，默认常用头部
	ExposeHeaders    []string      // 暴露给客户端的响应头
	AllowCredentials bool          // 是否允许携带凭证，默认 false
	MaxAge           time.Duration // 预检请求缓存时间，默认 12 小时
}

// defaultCORSConfig 默认 CORS 配置（安全优先）
func defaultCORSConfig() *CORSConfig {
	return &CORSConfig{
		AllowOrigins: []string{}, // 默认为空，需要显式配置
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowHeaders: []string{
			"Content-Type",
			"Authorization",
			"Cache-Control",
			"X-Requested-With",
		},
		ExposeHeaders:    []string{},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}
}

// CORSOption CORS 配置选项
type CORSOption func(*CORSConfig)

// WithAllowOrigins 设置允许的源
func WithAllowOrigins(origins ...string) CORSOption {
	return func(c *CORSConfig) {
		c.AllowOrigins = origins
	}
}

// WithAllowMethods 设置允许的方法
func WithAllowMethods(methods ...string) CORSOption {
	return func(c *CORSConfig) {
		c.AllowMethods = methods
	}
}

// WithAllowHeaders 设置允许的请求头
func WithAllowHeaders(headers ...string) CORSOption {
	return func(c *CORSConfig) {
		c.AllowHeaders = headers
	}
}

// WithExposeHeaders 设置暴露的响应头
func WithExposeHeaders(headers ...string) CORSOption {
	return func(c *CORSConfig) {
		c.ExposeHeaders = headers
	}
}

// WithAllowCredentials 设置是否允许凭证
func WithAllowCredentials(allow bool) CORSOption {
	return func(c *CORSConfig) {
		c.AllowCredentials = allow
	}
}

// WithMaxAge 设置预检请求缓存时间
func WithMaxAge(maxAge time.Duration) CORSOption {
	return func(c *CORSConfig) {
		c.MaxAge = maxAge
	}
}

// CORS 创建 CORS 中间件（需要显式配置源）
func CORS(options ...CORSOption) Middleware {
	config := defaultCORSConfig()
	for _, option := range options {
		option(config)
	}

	// 安全检查：不允许通配符源与凭证同时启用
	if config.AllowCredentials && slices.Contains(config.AllowOrigins, "*") {
		panic("CORS security error: cannot use wildcard origin with credentials")
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			origin := c.Request.Header.Get("Origin")

			// 处理预检请求
			if c.Request.Method == http.MethodOptions {
				handlePreflight(c, config, origin)
				return
			}

			// 处理实际请求
			handleActualRequest(c, config, origin)
			next(c)
		}
	}
}

// CORSDefault 创建默认 CORS 中间件（仅用于开发环境）
func CORSDefault() Middleware {
	return CORS(WithAllowOrigins("*"))
}

// handlePreflight 处理预检请求
func handlePreflight(c *gin.Context, config *CORSConfig, origin string) {
	// 检查源是否被允许
	if !isOriginAllowed(config.AllowOrigins, origin) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// 检查请求方法是否被允许
	requestMethod := c.Request.Header.Get("Access-Control-Request-Method")
	if requestMethod != "" && !slices.Contains(config.AllowMethods, requestMethod) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// 检查请求头是否被允许
	requestHeaders := c.Request.Header.Get("Access-Control-Request-Headers")
	if requestHeaders != "" && !areHeadersAllowed(config.AllowHeaders, requestHeaders) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// 设置 CORS 响应头
	setCORSHeaders(c, config, origin)
	c.AbortWithStatus(http.StatusNoContent)
}

// handleActualRequest 处理实际请求
func handleActualRequest(c *gin.Context, config *CORSConfig, origin string) {
	if isOriginAllowed(config.AllowOrigins, origin) {
		setCORSHeaders(c, config, origin)
	}
}

// setCORSHeaders 设置 CORS 响应头
func setCORSHeaders(c *gin.Context, config *CORSConfig, origin string) {
	// 设置允许的源
	if slices.Contains(config.AllowOrigins, "*") {
		c.Header("Access-Control-Allow-Origin", "*")
	} else if origin != "" {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
	}

	// 设置允许的方法
	if len(config.AllowMethods) > 0 {
		c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ", "))
	}

	// 设置允许的请求头
	if len(config.AllowHeaders) > 0 {
		c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ", "))
	}

	// 设置暴露的响应头
	if len(config.ExposeHeaders) > 0 {
		c.Header("Access-Control-Expose-Headers", strings.Join(config.ExposeHeaders, ", "))
	}

	// 设置是否允许凭证
	if config.AllowCredentials {
		c.Header("Access-Control-Allow-Credentials", "true")
	}

	// 设置预检请求缓存时间
	if config.MaxAge > 0 {
		c.Header("Access-Control-Max-Age", strconv.Itoa(int(config.MaxAge.Seconds())))
	}
}

// isOriginAllowed 检查源是否被允许
func isOriginAllowed(allowedOrigins []string, origin string) bool {
	if len(allowedOrigins) == 0 {
		return false // 默认不允许任何源
	}
	return slices.Contains(allowedOrigins, "*") || slices.Contains(allowedOrigins, origin)
}

// areHeadersAllowed 检查请求头是否被允许
func areHeadersAllowed(allowedHeaders []string, requestHeaders string) bool {
	headers := strings.Split(requestHeaders, ",")
	for _, header := range headers {
		header = strings.TrimSpace(header)
		if !isHeaderAllowed(allowedHeaders, header) {
			return false
		}
	}
	return true
}

// isHeaderAllowed 检查单个请求头是否被允许
func isHeaderAllowed(allowedHeaders []string, header string) bool {
	header = strings.ToLower(header)
	return slices.ContainsFunc(allowedHeaders, func(allowed string) bool {
		return strings.ToLower(allowed) == header
	})
}
