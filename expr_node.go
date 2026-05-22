package xltmpl

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// exprNode is a compiled expression for a single {{...}} cell expression.
// It supports:
//   - path  (e.g. .Foo.Bar or $.X)
//   - lit string ("...") / lit number
//   - call  (e.g. funcName arg1 arg2)
//
// Paths are resolved into step slices at parse time so that rendering never
// re-parses the expression.
type exprNode struct {
	kind    exprKind
	path    []string   // exprPath
	fn      evalFunc   // exprCall
	fnName  string     // exprCall (kept for error messages)
	args    []exprNode // exprCall
	litStr  string     // exprLitString
	litNum  float64    // exprLitNumber
	isFloat bool       // exprLitNumber: true for floats, false for ints
}

type exprKind uint8

const (
	exprPath exprKind = iota
	exprCall
	exprLitString
	exprLitNumber
)

// evalFunc is the signature of a built-in function. It receives args already
// evaluated, in order.
type evalFunc func(ctx *renderCtx, args []any) (any, error)

// parseExpr accepts the trimmed content between {{ and }} and returns a
// compiled exprNode.
func parseExpr(s string) (exprNode, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return exprNode{}, fmt.Errorf("empty expression")
	}
	// A path starts with . or $.
	if s[0] == '.' || s[0] == '$' {
		p, err := parsePath(s)
		if err != nil {
			return exprNode{}, err
		}
		return exprNode{kind: exprPath, path: p}, nil
	}
	// A function call: tokenize and resolve the function.
	tokens, err := tokenizeArgs(s)
	if err != nil {
		return exprNode{}, err
	}
	if len(tokens) == 0 {
		return exprNode{}, fmt.Errorf("expression is empty after tokenization")
	}
	name := tokens[0]
	fn, ok := lookupFunc(name)
	if !ok {
		return exprNode{}, fmt.Errorf("unknown function or variable %q", name)
	}
	args := make([]exprNode, 0, len(tokens)-1)
	for _, t := range tokens[1:] {
		an, err := parseArg(t)
		if err != nil {
			return exprNode{}, fmt.Errorf("arg %q: %w", t, err)
		}
		args = append(args, an)
	}
	return exprNode{kind: exprCall, fn: fn, fnName: name, args: args}, nil
}

// parseArg parses a single argument token: path, string literal "...", or number.
func parseArg(t string) (exprNode, error) {
	if t == "" {
		return exprNode{}, fmt.Errorf("empty argument")
	}
	if t[0] == '"' {
		if t[len(t)-1] != '"' {
			return exprNode{}, fmt.Errorf("missing closing \"")
		}
		raw := t[1 : len(t)-1]
		// Minimal escape support
		raw = strings.ReplaceAll(raw, `\"`, `"`)
		raw = strings.ReplaceAll(raw, `\\`, `\`)
		return exprNode{kind: exprLitString, litStr: raw}, nil
	}
	if t[0] == '.' || t[0] == '$' {
		p, err := parsePath(t)
		if err != nil {
			return exprNode{}, err
		}
		return exprNode{kind: exprPath, path: p}, nil
	}
	if isNumberLit(t) {
		if strings.ContainsAny(t, ".eE") {
			v, err := strconv.ParseFloat(t, 64)
			if err != nil {
				return exprNode{}, err
			}
			return exprNode{kind: exprLitNumber, litNum: v, isFloat: true}, nil
		}
		v, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			return exprNode{}, err
		}
		return exprNode{kind: exprLitNumber, litNum: float64(v), isFloat: false}, nil
	}
	return exprNode{}, fmt.Errorf("invalid token %q (expected path, literal, or number)", t)
}

func isNumberLit(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c == '-' || c == '+' || (c >= '0' && c <= '9')
}

// tokenizeArgs splits a string into tokens, honouring "quoted" substrings.
func tokenizeArgs(s string) ([]string, error) {
	var out []string
	i := 0
	for i < len(s) {
		// skip whitespace
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '"' {
			// string literal — accepts \" and \\
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' && j+1 < len(s) {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			if j > len(s) || (j == len(s) && s[j-1] != '"') {
				return nil, fmt.Errorf("missing closing \" in %q", s)
			}
			out = append(out, s[i:j])
			i = j
			continue
		}
		// Plain token — read until whitespace.
		j := i
		for j < len(s) && !unicode.IsSpace(rune(s[j])) {
			j++
		}
		out = append(out, s[i:j])
		i = j
	}
	return out, nil
}

// eval evaluates the expression node against the current render context.
func (e exprNode) eval(ctx *renderCtx) (any, error) {
	switch e.kind {
	case exprPath:
		return ctx.eval.resolve(e.path)
	case exprLitString:
		return e.litStr, nil
	case exprLitNumber:
		if e.isFloat {
			return e.litNum, nil
		}
		return int64(e.litNum), nil
	case exprCall:
		// Evaluate args before calling. Small stack buffer to avoid an allocation
		// when there are few arguments.
		var argBuf [4]any
		var args []any
		if len(e.args) <= len(argBuf) {
			args = argBuf[:len(e.args)]
		} else {
			args = make([]any, len(e.args))
		}
		for i := range e.args {
			v, err := e.args[i].eval(ctx)
			if err != nil {
				return nil, fmt.Errorf("%s: arg %d: %w", e.fnName, i+1, err)
			}
			args[i] = v
		}
		return e.fn(ctx, args)
	}
	return nil, fmt.Errorf("unknown exprNode kind: %d", e.kind)
}
