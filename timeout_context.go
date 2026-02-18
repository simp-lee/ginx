package ginx

// IMPORTANT: Gin version constraint
// This file uses unsafe reflection to access unexported gin.Context fields.
// It is only compatible with github.com/gin-gonic/gin v1.9.0 and above (pre-v2).
// The init() function at the bottom of this file validates field availability at
// startup, so an incompatible Gin upgrade will panic immediately rather than
// cause silent runtime failures.

import (
	"maps"
	"net/http"
	"reflect"
	"unsafe"

	"github.com/gin-gonic/gin"
)

func cloneContextForTimeout(c *gin.Context, writer gin.ResponseWriter, req *http.Request) *gin.Context {
	cloned := c.Copy()
	cloneTimeoutExecutionFields(cloned, c)
	cloned.Writer = writer
	cloned.Request = req

	if c.Keys != nil {
		cloned.Keys = make(map[any]any, len(c.Keys))
		maps.Copy(cloned.Keys, c.Keys)
	}

	if len(c.Params) > 0 {
		cloned.Params = make(gin.Params, len(c.Params))
		copy(cloned.Params, c.Params)
	}

	if len(c.Errors) > 0 {
		cloned.Errors = append(cloned.Errors[:0:0], c.Errors...)
	}

	if len(c.Accepted) > 0 {
		cloned.Accepted = append([]string(nil), c.Accepted...)
	}

	return cloned
}

// cloneTimeoutExecutionFields copies Gin's unexported handler execution fields from src to dst.
// This uses unsafe reflection to access internal gin.Context fields that are not part of Gin's
// public API. The validity of these fields is checked at init time (see init() below).
//
// Gin version constraint: requires github.com/gin-gonic/gin v1.9.x and above (validated at init).
// A Gin upgrade that renames or removes these fields will cause a panic at application startup
// (via the init check), preventing silent runtime failures.
func cloneTimeoutExecutionFields(dst *gin.Context, src *gin.Context) {
	if dst == nil || src == nil {
		return
	}

	for _, field := range []string{"handlers", "index", "fullPath", "engine", "params", "skippedNodes"} {
		copyUnexportedContextField(dst, src, field)
	}
}

// copyUnexportedContextField uses unsafe reflection to copy a single unexported field
// from src to dst gin.Context. This bypasses Go's type system — see the init() check
// and cloneTimeoutExecutionFields documentation for safety constraints.
func copyUnexportedContextField(dst *gin.Context, src *gin.Context, fieldName string) {
	dstField := reflect.ValueOf(dst).Elem().FieldByName(fieldName)
	srcField := reflect.ValueOf(src).Elem().FieldByName(fieldName)
	if !dstField.IsValid() || !srcField.IsValid() {
		return
	}

	dstValue := reflect.NewAt(dstField.Type(), unsafe.Pointer(dstField.UnsafeAddr())).Elem()
	srcValue := reflect.NewAt(srcField.Type(), unsafe.Pointer(srcField.UnsafeAddr())).Elem()
	dstValue.Set(srcValue)
}

// init validates that the unexported gin.Context fields required by the timeout middleware
// still exist. This catches incompatible Gin upgrades at application startup rather than
// at request time where failures would be silent and potentially dangerous.
func init() {
	var c gin.Context
	v := reflect.ValueOf(&c).Elem()
	for _, name := range []string{"handlers", "index", "fullPath", "engine", "params", "skippedNodes"} {
		if !v.FieldByName(name).IsValid() {
			panic("ginx: gin.Context missing expected internal field '" + name + "'; this ginx version may be incompatible with the installed gin version")
		}
	}
}

func syncContextFromTimeoutExecution(dst *gin.Context, src *gin.Context) {
	if dst == nil || src == nil {
		return
	}

	if src.Keys != nil {
		dst.Keys = make(map[any]any, len(src.Keys))
		maps.Copy(dst.Keys, src.Keys)
	} else {
		dst.Keys = nil
	}

	dst.Params = src.Params
	dst.Errors = src.Errors
	dst.Accepted = src.Accepted
}
