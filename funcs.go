package xltmpl

import (
	"fmt"
	"strings"
	"time"
)

// funcRegistry maps built-in function names to their implementations.
// Extend it at runtime via RegisterFunc.
var funcRegistry = map[string]evalFunc{
	// --- Context variables (zero-arg) ---
	"currentRow": func(ctx *renderCtx, _ []any) (any, error) {
		return ctx.outRow, nil
	},
	"currentCol": func(ctx *renderCtx, _ []any) (any, error) {
		return ctx.outCol, nil
	},
	// index: 0-based index of the innermost range loop.
	"index": func(ctx *renderCtx, _ []any) (any, error) {
		if len(ctx.loopIdx) == 0 {
			return 0, nil
		}
		return ctx.loopIdx[len(ctx.loopIdx)-1], nil
	},
	// index1: 1-based index of the innermost range loop.
	"index1": func(ctx *renderCtx, _ []any) (any, error) {
		if len(ctx.loopIdx) == 0 {
			return 1, nil
		}
		return ctx.loopIdx[len(ctx.loopIdx)-1] + 1, nil
	},
	// Aliases / convenience accessors.
	"rowNum": func(ctx *renderCtx, _ []any) (any, error) {
		return ctx.outRow, nil
	},
	"colName": func(ctx *renderCtx, _ []any) (any, error) {
		return indexToColName(ctx.outCol), nil
	},

	// --- Time ---
	"unixTime":   fnUnixTime, // seconds + tz + layout
	"unixMillis": fnUnixMillis,
	"formatTime": fnFormatTime,
	"now":        fnNow,

	// --- String ---
	"upper":   fnUpper,
	"lower":   fnLower,
	"concat":  fnConcat,
	"default": fnDefault,
	"sprintf": fnSprintf,

	// --- Math (preserve int when both operands are int) ---
	"add": fnAdd,
	"sub": fnSub,
	"mul": fnMul,
	"div": fnDiv,
}

func lookupFunc(name string) (evalFunc, bool) {
	fn, ok := funcRegistry[name]
	return fn, ok
}

// RegisterFunc registers a user-defined function under `name`. Call it before
// ParseFile so the parser can resolve the name. The function name must not
// collide with an existing built-in.
func RegisterFunc(name string, fn func(args []any) (any, error)) error {
	if _, exists := funcRegistry[name]; exists {
		return fmt.Errorf("xltmpl: function %q already exists", name)
	}
	funcRegistry[name] = func(_ *renderCtx, args []any) (any, error) {
		return fn(args)
	}
	return nil
}

// --- Implementations ---

func fnUnixTime(_ *renderCtx, args []any) (any, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("unixTime: expected 1..3 args (ts, [tz], [layout])")
	}
	sec, err := toInt64(args[0])
	if err != nil {
		return nil, fmt.Errorf("unixTime: ts: %w", err)
	}
	tz := "UTC"
	layout := "2006-01-02 15:04:05"
	if len(args) >= 2 {
		tz = toString(args[1])
	}
	if len(args) >= 3 {
		layout = toString(args[2])
	}
	loc, err := loadLocation(tz)
	if err != nil {
		return nil, err
	}
	return time.Unix(sec, 0).In(loc).Format(layout), nil
}

func fnUnixMillis(_ *renderCtx, args []any) (any, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("unixMillis: expected 1..3 args (ts_ms, [tz], [layout])")
	}
	ms, err := toInt64(args[0])
	if err != nil {
		return nil, err
	}
	tz := "UTC"
	layout := "2006-01-02 15:04:05"
	if len(args) >= 2 {
		tz = toString(args[1])
	}
	if len(args) >= 3 {
		layout = toString(args[2])
	}
	loc, err := loadLocation(tz)
	if err != nil {
		return nil, err
	}
	return time.UnixMilli(ms).In(loc).Format(layout), nil
}

func fnFormatTime(_ *renderCtx, args []any) (any, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("formatTime: expected 1..3 args (t, [tz], [layout])")
	}
	t, err := toTime(args[0])
	if err != nil {
		return nil, err
	}
	tz := ""
	layout := "2006-01-02 15:04:05"
	if len(args) >= 2 {
		tz = toString(args[1])
	}
	if len(args) >= 3 {
		layout = toString(args[2])
	}
	if tz != "" {
		loc, err := loadLocation(tz)
		if err != nil {
			return nil, err
		}
		t = t.In(loc)
	}
	return t.Format(layout), nil
}

func fnNow(_ *renderCtx, _ []any) (any, error) { return time.Now(), nil }

func fnUpper(_ *renderCtx, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("upper: expected 1 arg")
	}
	return strings.ToUpper(toString(args[0])), nil
}

func fnLower(_ *renderCtx, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("lower: expected 1 arg")
	}
	return strings.ToLower(toString(args[0])), nil
}

func fnConcat(_ *renderCtx, args []any) (any, error) {
	var b strings.Builder
	for _, a := range args {
		b.WriteString(toString(a))
	}
	return b.String(), nil
}

func fnDefault(_ *renderCtx, args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("default: expected 2 args (value, fallback)")
	}
	if isEmpty(args[0]) {
		return args[1], nil
	}
	return args[0], nil
}

func fnSprintf(_ *renderCtx, args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("sprintf: expected at least 1 arg")
	}
	format := toString(args[0])
	return fmt.Sprintf(format, args[1:]...), nil
}

func fnAdd(_ *renderCtx, args []any) (any, error) { return mathOp("add", args) }
func fnSub(_ *renderCtx, args []any) (any, error) { return mathOp("sub", args) }
func fnMul(_ *renderCtx, args []any) (any, error) { return mathOp("mul", args) }
func fnDiv(_ *renderCtx, args []any) (any, error) { return mathOp("div", args) }

func mathOp(op string, args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s: expected 2 args", op)
	}
	aF, aIsInt, err := numericValue(args[0])
	if err != nil {
		return nil, err
	}
	bF, bIsInt, err := numericValue(args[1])
	if err != nil {
		return nil, err
	}
	bothInt := aIsInt && bIsInt && op != "div"
	var r float64
	switch op {
	case "add":
		r = aF + bF
	case "sub":
		r = aF - bF
	case "mul":
		r = aF * bF
	case "div":
		if bF == 0 {
			return nil, fmt.Errorf("div: division by zero")
		}
		r = aF / bF
	}
	if bothInt {
		return int64(r), nil
	}
	return r, nil
}

// --- Conversion helpers ---

func loadLocation(tz string) (*time.Location, error) {
	if tz == "" || strings.EqualFold(tz, "UTC") {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return loc, nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case time.Time:
		return s.Format(time.RFC3339)
	}
	return fmt.Sprintf("%v", v)
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case uint:
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		return int64(x), nil
	case float32:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case string:
		// Allow unix timestamps passed as strings.
		var n int64
		_, err := fmt.Sscanf(x, "%d", &n)
		if err != nil {
			return 0, fmt.Errorf("not a number: %q", x)
		}
		return n, nil
	}
	return 0, fmt.Errorf("cannot convert %T to int64", v)
}

func toTime(v any) (time.Time, error) {
	switch x := v.(type) {
	case time.Time:
		return x, nil
	case int64:
		return time.Unix(x, 0), nil
	case int:
		return time.Unix(int64(x), 0), nil
	}
	return time.Time{}, fmt.Errorf("cannot convert %T to time.Time", v)
}

// numericValue returns a float64 plus an isInt flag, used by math helpers to
// decide whether to return an int or a float result.
func numericValue(v any) (float64, bool, error) {
	switch x := v.(type) {
	case int:
		return float64(x), true, nil
	case int64:
		return float64(x), true, nil
	case int32:
		return float64(x), true, nil
	case float64:
		return x, false, nil
	case float32:
		return float64(x), false, nil
	}
	return 0, false, fmt.Errorf("not a number: %T", v)
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return x == ""
	case int:
		return x == 0
	case int64:
		return x == 0
	case float64:
		return x == 0
	case bool:
		return !x
	}
	return false
}
