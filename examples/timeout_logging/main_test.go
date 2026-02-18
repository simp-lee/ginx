package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTimeoutLoggingExampleRoutes(t *testing.T) {
	r := newRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
