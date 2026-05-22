package xltmpl

import (
	"fmt"
	"reflect"
)

// evalCtx is the data context used while evaluating a single expression:
//   - root: the original data passed to Render (map or struct)
//   - cur : the current item inside a loop (nil outside any loop -> use root)
type evalCtx struct {
	root any
	cur  any
}

// resolve walks a pre-parsed path on the current context.
//
//   - path == nil       -> returns cur (or root if cur is nil)
//   - path[0] == "$"    -> start from root
//   - otherwise         -> start from cur (or root if cur is nil)
//
// Intermediate values are auto-dereferenced through pointers/interfaces, and
// each step reads a key from a map[string]any or a field from a struct by name.
func (e evalCtx) resolve(path []string) (any, error) {
	if len(path) == 0 {
		if e.cur != nil {
			return e.cur, nil
		}
		return e.root, nil
	}
	var cur any
	start := 0
	if path[0] == "$" {
		cur = e.root
		start = 1
	} else {
		cur = e.cur
		if cur == nil {
			cur = e.root
		}
	}
	for i := start; i < len(path); i++ {
		v, err := lookup(cur, path[i])
		if err != nil {
			return nil, err
		}
		cur = v
	}
	return cur, nil
}

func lookup(obj any, key string) (any, error) {
	if obj == nil {
		return nil, nil
	}
	// Fast path: map[string]any
	if m, ok := obj.(map[string]any); ok {
		return m[key], nil
	}
	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map:
		k := reflect.ValueOf(key)
		if !k.Type().AssignableTo(v.Type().Key()) {
			return nil, fmt.Errorf("xltmpl: incompatible map key type for %q", key)
		}
		mv := v.MapIndex(k)
		if !mv.IsValid() {
			return nil, nil
		}
		return mv.Interface(), nil
	case reflect.Struct:
		f := v.FieldByName(key)
		if !f.IsValid() {
			return nil, fmt.Errorf("xltmpl: field %q not found", key)
		}
		return f.Interface(), nil
	}
	return nil, fmt.Errorf("xltmpl: cannot access %q on value of type %T", key, obj)
}

// asSlice coerces a value into []any so the renderer can iterate it.
// Supports []any, []map[string]any, and any other slice/array via reflect.
func asSlice(v any) ([]any, error) {
	if v == nil {
		return nil, nil
	}
	switch s := v.(type) {
	case []any:
		return s, nil
	case []map[string]any:
		out := make([]any, len(s))
		for i, x := range s {
			out[i] = x
		}
		return out, nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		n := rv.Len()
		out := make([]any, n)
		for i := 0; i < n; i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out, nil
	}
	return nil, fmt.Errorf("xltmpl: expected slice/array, got %T", v)
}
