package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
)

func main() {
	r := gin.New()

	// Place RequestID first so all downstream middlewares/handlers can use the id
	chain := ginx.NewChain().
		Use(ginx.RequestID()).
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

	r.Run(":8080")
}
