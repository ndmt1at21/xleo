package xltmpl

import (
	"fmt"
	"strings"
	"unicode"
)

// parseCell splits a cell's content into literal/expression segments and
// detects whether the cell is a control marker (range/end/hrange/hend).
//
// Returns:
//   - segs: nil when the cell is a marker (markerKind != markerNone)
//   - markerKind: which kind of marker was found
//   - markerPath: the path argument for range/hrange markers
func parseCell(raw string) (segs []seg, markerKind markerType, markerPath []string, err error) {
	trim := strings.TrimSpace(raw)
	// A marker cell contains exactly one {{ ... }} directive and nothing else.
	if strings.HasPrefix(trim, "{{") && strings.HasSuffix(trim, "}}") {
		inner := strings.TrimSpace(trim[2 : len(trim)-2])
		if k, p, ok := matchMarker(inner); ok {
			return nil, k, p, nil
		}
	}
	// Otherwise tokenize the cell into segments.
	segs, err = tokenize(raw)
	return segs, markerNone, nil, err
}

type markerType uint8

const (
	markerNone markerType = iota
	markerRange
	markerEnd
	markerHRange
	markerHEnd
)

func matchMarker(inner string) (markerType, []string, bool) {
	// {{end}} / {{hend}}
	switch inner {
	case "end":
		return markerEnd, nil, true
	case "hend":
		return markerHEnd, nil, true
	}
	// {{range .Path}} or {{hrange .Path}}
	if rest, ok := cutPrefixWord(inner, "range"); ok {
		path, err := parsePath(strings.TrimSpace(rest))
		if err == nil {
			return markerRange, path, true
		}
	}
	if rest, ok := cutPrefixWord(inner, "hrange"); ok {
		path, err := parsePath(strings.TrimSpace(rest))
		if err == nil {
			return markerHRange, path, true
		}
	}
	return markerNone, nil, false
}

// cutPrefixWord returns the remainder after prefix if prefix is a leading word
// (followed by whitespace or end of string).
func cutPrefixWord(s, word string) (string, bool) {
	if !strings.HasPrefix(s, word) {
		return "", false
	}
	rest := s[len(word):]
	if rest == "" {
		return rest, true
	}
	if unicode.IsSpace(rune(rest[0])) {
		return rest, true
	}
	return "", false
}

// tokenize turns a cell value into segments. Each {{...}} pair becomes an
// expression segment; the surrounding characters become literal segments.
func tokenize(s string) ([]seg, error) {
	if s == "" {
		return nil, nil
	}
	if !strings.Contains(s, "{{") {
		return []seg{{text: s}}, nil
	}
	out := make([]seg, 0, 4)
	i := 0
	for i < len(s) {
		open := strings.Index(s[i:], "{{")
		if open < 0 {
			out = append(out, seg{text: s[i:]})
			break
		}
		open += i
		if open > i {
			out = append(out, seg{text: s[i:open]})
		}
		close := strings.Index(s[open+2:], "}}")
		if close < 0 {
			return nil, fmt.Errorf("xltmpl: missing closing }} in %q", s)
		}
		exprStr := strings.TrimSpace(s[open+2 : open+2+close])
		ex, err := parseExpr(exprStr)
		if err != nil {
			return nil, fmt.Errorf("xltmpl: invalid expression %q: %w", exprStr, err)
		}
		out = append(out, seg{isExpr: true, expr: ex})
		i = open + 2 + close + 2
	}
	return out, nil
}

// parsePath turns a path string into a slice of access steps.
//
//	"."          -> nil          (the current item itself)
//	".Foo.Bar"   -> ["Foo", "Bar"]
//	"$"          -> ["$"]        (the root context)
//	"$.X.Y"      -> ["$", "X", "Y"]
//
// Arithmetic/function syntax is not handled here — that lives in parseExpr.
func parsePath(expr string) ([]string, error) {
	if expr == "" {
		return nil, fmt.Errorf("empty expression")
	}
	if expr == "." {
		return nil, nil
	}
	if expr == "$" {
		return []string{"$"}, nil
	}
	root := "."
	rest := expr
	if strings.HasPrefix(expr, "$") {
		root = "$"
		rest = expr[1:]
	}
	if !strings.HasPrefix(rest, ".") {
		return nil, fmt.Errorf("path must start with . or $.")
	}
	rest = rest[1:]
	parts := strings.Split(rest, ".")
	out := make([]string, 0, len(parts)+1)
	if root == "$" {
		out = append(out, "$")
	}
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("invalid path: empty segment")
		}
		out = append(out, p)
	}
	return out, nil
}
