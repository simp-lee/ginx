package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
)

func TestExtractUserIDUsesTypedContextKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("should set query user id via ginx.SetUserID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/basic/per-user?user_id=user123", nil)

		h := extractUserID()
		h(c)

		userID, ok := ginx.GetUserID(c)
		if !ok {
			t.Fatalf("expected user id in typed context key")
		}
		if userID != "user123" {
			t.Fatalf("got user id %q, want %q", userID, "user123")
		}
	})

	t.Run("should default to anonymous when query is missing", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/basic/per-user", nil)

		h := extractUserID()
		h(c)

		userID, ok := ginx.GetUserID(c)
		if !ok {
			t.Fatalf("expected user id in typed context key")
		}
		if userID != "anonymous" {
			t.Fatalf("got user id %q, want %q", userID, "anonymous")
		}
	})
}
