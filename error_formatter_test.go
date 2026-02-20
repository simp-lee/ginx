package ginx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAbortWithError_WithFormatter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	formatter := func(status int, msg string) any {
		return map[string]any{"code": status, "message": msg, "data": nil}
	}
	SetErrorFormatter(c, formatter)

	AbortWithError(c, 401, "missing token")

	assert.Equal(t, 401, w.Code)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assert.NoError(t, err)
	assert.Equal(t, float64(401), body["code"])
	assert.Equal(t, "missing token", body["message"])
	assert.Nil(t, body["data"])
}

func TestAbortWithError_WithoutFormatter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	AbortWithError(c, 500, "something went wrong")

	assert.Equal(t, 500, w.Code)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assert.NoError(t, err)
	assert.Equal(t, "something went wrong", body["error"])
	// Should not contain formatter-specific keys
	_, hasCode := body["code"]
	assert.False(t, hasCode, "default response should not contain 'code' key")
}

func TestSetGetErrorFormatter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns nil when not set", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		f := GetErrorFormatter(c)
		assert.Nil(t, f)
	})

	t.Run("set and get returns same formatter", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		formatter := func(status int, msg string) any {
			return map[string]any{"err": msg}
		}
		SetErrorFormatter(c, formatter)

		got := GetErrorFormatter(c)
		assert.NotNil(t, got)

		// Verify functional equivalence: same input produces same output
		expected := formatter(400, "bad request")
		actual := got(400, "bad request")
		assert.Equal(t, expected, actual)
	})
}

func TestChainWithErrorFormat_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	formatter := func(status int, msg string) any {
		return map[string]any{"code": status, "message": msg, "data": nil}
	}

	mockJWT := new(MockJWTService)

	router := gin.New()
	chain := NewChain().
		WithErrorFormat(formatter).
		Use(Auth(mockJWT))

	router.GET("/protected", chain.Build(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	// Send request without Authorization header to trigger Auth 401
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assert.NoError(t, err)

	// Expect formatter output, not default {"error":"..."}
	assert.Equal(t, float64(401), body["code"])
	assert.Equal(t, "missing token", body["message"])
	assert.Nil(t, body["data"])
	_, hasError := body["error"]
	assert.False(t, hasError, "should use formatter, not default error format")
}

func TestErrorFormatMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	formatter := func(status int, msg string) any {
		return map[string]any{"status": status, "detail": msg}
	}

	mockJWT := new(MockJWTService)

	router := gin.New()
	chain := NewChain().
		Use(ErrorFormat(formatter)).
		Use(Auth(mockJWT))

	router.GET("/secure", chain.Build(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	// No token → Auth middleware returns 401 via AbortWithError
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assert.NoError(t, err)
	assert.Equal(t, float64(401), body["status"])
	assert.Equal(t, "missing token", body["detail"])
}

func TestRecoveryWithErrorFormatter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("panic response uses formatter when set", func(t *testing.T) {
		formatter := func(status int, msg string) any {
			return map[string]any{"code": status, "message": msg, "data": nil}
		}

		router := gin.New()
		chain := NewChain().
			WithErrorFormat(formatter).
			Use(Recovery())

		router.GET("/panic", chain.Build(), func(c *gin.Context) {
			panic("test panic")
		})

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 500, w.Code)

		var body map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &body)
		assert.NoError(t, err)
		assert.Equal(t, float64(500), body["code"])
		assert.Equal(t, "internal server error", body["message"])
		assert.Nil(t, body["data"])
		_, hasError := body["error"]
		assert.False(t, hasError, "should use formatter, not default error format")
	})

	t.Run("panic response uses default when no formatter", func(t *testing.T) {
		router := gin.New()
		chain := NewChain().
			Use(Recovery())

		router.GET("/panic", chain.Build(), func(c *gin.Context) {
			panic("test panic")
		})

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 500, w.Code)

		var body map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &body)
		assert.NoError(t, err)
		assert.Equal(t, "internal server error", body["error"])
		_, hasCode := body["code"]
		assert.False(t, hasCode, "default response should not contain 'code' key")
	})
}

func TestErrorFormatterPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	firstFormatter := func(status int, msg string) any {
		return map[string]any{"source": "first", "error": msg}
	}
	secondFormatter := func(status int, msg string) any {
		return map[string]any{"source": "second", "error": msg}
	}

	SetErrorFormatter(c, firstFormatter)
	SetErrorFormatter(c, secondFormatter)

	AbortWithError(c, 400, "bad request")

	assert.Equal(t, 400, w.Code)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assert.NoError(t, err)
	assert.Equal(t, "second", body["source"], "later formatter should override earlier one")
	assert.Equal(t, "bad request", body["error"])
}
