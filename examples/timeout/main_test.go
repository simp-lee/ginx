package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTimeoutExampleFastRoute(t *testing.T) {
	r := newRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestTimeout {
		t.Fatalf("expected 408 from /fast timeout, got %d", w.Code)
	}
}
