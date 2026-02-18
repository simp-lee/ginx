package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/simp-lee/ginx"
)

func TestCombinedExampleRoutes(t *testing.T) {
	r := newRouter()
	defer ginx.CleanupRateLimiters()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
