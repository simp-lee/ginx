package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
)

// requestIDKey is the context key for storing request_id in context.Context.
type requestIDKey struct{}

func main() {
	// Set up a structured JSON logger so request_id appears in log output
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	r := newRouter()
	r.Run(":8080")
}

func newRouter() *gin.Engine {
	r := gin.New()

	// Place RequestID first so all downstream middlewares/handlers can use the id.
	// WithContextInjector injects request_id into Go's context.Context, making it
	// available to service/repository layers that only receive context.Context.
	chain := ginx.NewChain().
		Use(ginx.RequestID(ginx.WithContextInjector(func(ctx context.Context, id string) context.Context {
			return context.WithValue(ctx, requestIDKey{}, id)
		}))).
		Use(ginx.Recovery()).
		Use(ginx.Logger()).
		Use(ginx.Timeout(ginx.WithTimeout(5 * time.Second)))
	r.Use(chain.Build())

	// Simple health endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Echo request id from context and response header
	r.GET("/whoami", func(c *gin.Context) {
		rid, _ := ginx.GetRequestID(c)
		c.JSON(http.StatusOK, gin.H{
			"message":    "hello",
			"request_id": rid,
		})
	})

	// Demonstrate respecting incoming X-Request-ID (default behavior)
	// Try: curl -H "X-Request-ID: req-demo-123" http://localhost:8080/whoami

	// Demonstrate ignoring incoming header using a route-specific chain
	r.GET("/newid",
		ginx.NewChain().Use(ginx.RequestID(ginx.WithIgnoreIncoming())).Build(),
		func(c *gin.Context) {
			rid, _ := ginx.GetRequestID(c)
			c.JSON(http.StatusOK, gin.H{"request_id": rid, "note": "always regenerated"})
		},
	)

	// Demonstrate WithContextInjector: the request_id is stored in context.Context
	// and can be retrieved by service-layer code that only has context.Context.
	// Try: curl http://localhost:8080/log (check server stdout for structured log with request_id)
	r.GET("/log", func(c *gin.Context) {
		// Simulate a service call that only receives context.Context, not *gin.Context
		handleLog(c.Request.Context())
		rid, _ := ginx.GetRequestID(c)
		c.JSON(http.StatusOK, gin.H{"message": "check server logs", "request_id": rid})
	})

	return r
}

// handleLog simulates a service-layer function that only has access to context.Context.
// Thanks to WithContextInjector, the request_id is available in the context.
func handleLog(ctx context.Context) {
	rid, _ := ctx.Value(requestIDKey{}).(string)
	slog.InfoContext(ctx, "processing request in service layer", "request_id", rid)
}
