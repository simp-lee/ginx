package ginx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/logger"
)

// RecoveryHandler 定义 panic 恢复处理函数类型
type RecoveryHandler func(*gin.Context, any)

// defaultRecoveryHandler 默认的 panic 恢复处理
func defaultRecoveryHandler(c *gin.Context, err any) {
	// 简单统一返回 JSON 格式，适用于现代 Web 开发
	c.AbortWithStatusJSON(500, gin.H{
		"error":   "Internal Server Error",
		"message": "An unexpected error occurred",
	})
}

// Recovery 创建 panic 恢复中间件
func Recovery(options ...logger.Option) Middleware {
	return RecoveryWith(nil, options...)
}

// RecoveryWith 创建带自定义处理器的 panic 恢复中间件
func RecoveryWith(handler RecoveryHandler, loggerOptions ...logger.Option) Middleware {
	// 创建 logger 实例
	log, err := logger.New(loggerOptions...)
	if err != nil {
		panic("failed to create logger for recovery: " + err.Error())
	}

	// 使用默认处理器（如果没有提供）
	if handler == nil {
		handler = defaultRecoveryHandler
	}

	return func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			defer func() {
				if err := recover(); err != nil {
					// 检查是否为网络连接断开错误
					brokenPipe := isBrokenPipe(err)

					// 记录 panic 日志
					if brokenPipe {
						// 网络断开只记录基本信息，不记录堆栈
						log.Warn("Connection broken",
							"error", fmt.Sprintf("%v", err),
							"path", c.Request.URL.Path,
							"method", c.Request.Method,
							"ip", c.ClientIP(),
						)
						// 网络断开时不能写入响应，直接中止
						c.Error(err.(error))
						c.Abort()
					} else {
						// 真正的 panic 记录完整堆栈信息
						stack := getStack()
						log.Error("Panic recovered",
							"error", fmt.Sprintf("%v", err),
							"path", c.Request.URL.Path,
							"method", c.Request.Method,
							"ip", c.ClientIP(),
							"user_agent", c.Request.UserAgent(),
							"stack", stack,
						)
						// 调用恢复处理器
						handler(c, err)
					}
				}
			}()

			// 执行下一个中间件
			next(c)
		}
	}
}

// getStack 获取当前堆栈信息
func getStack() string {
	var buf [4096]byte
	n := runtime.Stack(buf[:], false)
	stack := string(buf[:n])

	// 过滤掉 recovery 中间件相关的堆栈帧
	lines := strings.Split(stack, "\n")
	var filteredLines []string
	skipNext := false

	for _, line := range lines {
		// 跳过包含 recovery.go 的行及其下一行（文件位置）
		if strings.Contains(line, "recovery.go") {
			skipNext = true
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		// 跳过 runtime panic 相关的行
		if strings.Contains(line, "runtime.gopanic") ||
			strings.Contains(line, "runtime/panic.go") {
			continue
		}

		// 清理函数名（借鉴 Gin 的做法）
		line = cleanFunctionName(line)
		filteredLines = append(filteredLines, line)
	}

	return strings.Join(filteredLines, "\n")
}

// cleanFunctionName 清理函数名，移除包路径并修复特殊字符
func cleanFunctionName(line string) string {
	// 只处理函数名行（不包含文件路径的行）
	if strings.Contains(line, "/") && strings.Contains(line, ":") {
		return line // 这是文件路径行，不处理
	}

	// 移除包路径（最后一个斜杠后的内容）
	if lastSlash := strings.LastIndexByte(line, '/'); lastSlash >= 0 {
		before := line[:strings.LastIndexByte(line[:lastSlash], ' ')+1]
		after := line[lastSlash+1:]
		line = before + after
	}

	// 修复中心点符号
	line = strings.ReplaceAll(line, "·", ".")

	return line
}

// isBrokenPipe 检查错误是否为网络连接断开
func isBrokenPipe(err any) bool {
	if ne, ok := err.(*net.OpError); ok {
		var se *os.SyscallError
		if errors.As(ne, &se) {
			seStr := strings.ToLower(se.Error())
			return strings.Contains(seStr, "broken pipe") ||
				strings.Contains(seStr, "connection reset by peer")
		}
	}
	return false
}
