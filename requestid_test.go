package ginx

import (
	"net/http"
	"regexp"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func isHex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	matched, _ := regexp.MatchString("^[0-9a-f]{32}$", s)
	return matched
}

func TestRequestID_BasicGeneration(t *testing.T) {
	m := RequestID()

	next := func(c *gin.Context) {
		// request id should be available before next
		if id, ok := GetRequestID(c); !ok || id == "" {
			t.Error("request id should be set in context before next")
		}
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
	}

	c, w := TestContext("GET", "/api/ping", nil)
	m(next)(c)

	rid := w.Header().Get("X-Request-ID")
	if rid == "" {
		t.Fatal("X-Request-ID should be set in response header")
	}
	if !isHex32(rid) {
		t.Errorf("generated request id should be 32-hex string, got %q", rid)
	}

	ctxID, ok := GetRequestID(c)
	if !ok || ctxID != rid {
		t.Error("context request id should equal response header value")
	}
}

func TestRequestID_RespectIncomingDefault(t *testing.T) {
	const incoming = "req-incoming-123"
	m := RequestID() // default: RespectIncoming=true

	next := func(c *gin.Context) {
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
	}

	c, w := TestContext("GET", "/api/test", map[string]string{
		"X-Request-ID": incoming,
	})
	m(next)(c)

	if w.Header().Get("X-Request-ID") != incoming {
		t.Errorf("should respect incoming id, want %q got %q", incoming, w.Header().Get("X-Request-ID"))
	}
	if id, _ := GetRequestID(c); id != incoming {
		t.Errorf("context id should equal incoming, want %q got %q", incoming, id)
	}
}

func TestRequestID_IgnoreIncoming(t *testing.T) {
	const incoming = "incoming-not-used"
	m := RequestID(WithIgnoreIncoming())

	next := func(c *gin.Context) {
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
	}

	c, w := TestContext("GET", "/api/test", map[string]string{
		"X-Request-ID": incoming,
	})
	m(next)(c)

	rid := w.Header().Get("X-Request-ID")
	if rid == incoming {
		t.Error("should override incoming id when WithIgnoreIncoming is set")
	}
	if !isHex32(rid) {
		t.Errorf("overridden id should be 32-hex, got %q", rid)
	}
}

func TestRequestID_CustomHeaderName(t *testing.T) {
	const header = "X-Correlation-ID"
	const incoming = "corr-abc"
	m := RequestID(WithRequestIDHeader(header))

	next := func(c *gin.Context) {
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
	}

	c, w := TestContext("GET", "/api/test", map[string]string{header: incoming})
	m(next)(c)

	if w.Header().Get(header) != incoming {
		t.Errorf("should echo custom header, want %q got %q", incoming, w.Header().Get(header))
	}
	// default header should not be set
	if w.Header().Get("X-Request-ID") != "" {
		t.Error("default X-Request-ID should not be set when using custom header")
	}
}

func TestRequestID_ExposeHeaders_SetWhenEmpty(t *testing.T) {
	m := RequestID()

	next := func(c *gin.Context) {
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
	}

	c, w := TestContext("GET", "/api/test", nil)
	m(next)(c)

	expose := w.Header().Get("Access-Control-Expose-Headers")
	if expose == "" || expose != "X-Request-ID" {
		t.Errorf("Expose-Headers should include X-Request-ID, got %q", expose)
	}
}

func TestRequestID_ExposeHeaders_AppendsWithoutDup(t *testing.T) {
	m := RequestID()
	next := func(c *gin.Context) {
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
	}

	c, w := TestContext("GET", "/api/test", nil)

	// pre-set existing expose headers BEFORE RequestID executes
	preAndRun := func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Expose-Headers", "X-Trace-ID")
		m(next)(ctx)
	}

	preAndRun(c)

	expose := w.Header().Get("Access-Control-Expose-Headers")
	if expose != "X-Trace-ID, X-Request-ID" {
		t.Errorf("should append X-Request-ID to existing expose headers, got %q", expose)
	}

	// now test no-duplication when already present
	c2, w2 := TestContext("GET", "/api/test", nil)
	preAndRun2 := func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		m(next)(ctx)
	}
	preAndRun2(c2)
	expose2 := w2.Header().Get("Access-Control-Expose-Headers")
	if expose2 != "X-Request-ID" {
		t.Errorf("should not duplicate existing header, got %q", expose2)
	}
}

func TestRequestID_HasRequestID_Condition(t *testing.T) {
	cond := HasRequestID()

	// before middleware: false
	c1, _ := TestContext("GET", "/api/test", nil)
	if cond(c1) {
		t.Error("HasRequestID should be false before middleware sets id")
	}

	// after middleware: true
	m := RequestID()
	next := func(c *gin.Context) { c.Status(http.StatusOK); c.Writer.WriteHeaderNow() }
	c2, _ := TestContext("GET", "/api/test", nil)
	m(next)(c2)
	if !cond(c2) {
		t.Error("HasRequestID should be true after middleware")
	}
}

func TestRequestID_GetFromHeader_Helper(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "abc")
	if v := GetRequestIDFromHeader(req, ""); v != "abc" {
		t.Errorf("expected abc, got %q", v)
	}
	req.Header.Set("X-Correlation-ID", "xyz")
	if v := GetRequestIDFromHeader(req, "X-Correlation-ID"); v != "xyz" {
		t.Errorf("expected xyz, got %q", v)
	}
}

func TestRequestID_Concurrency(t *testing.T) {
	m := RequestID()
	next := func(c *gin.Context) { c.Status(http.StatusOK); c.Writer.WriteHeaderNow() }

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)

	errs := make(chan string, N)

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			c, w := TestContext("GET", "/api/test", nil)
			m(next)(c)
			rid := w.Header().Get("X-Request-ID")
			if !isHex32(rid) {
				errs <- "invalid id"
			}
		}()
	}

	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrency error: %s", e)
	}
}
