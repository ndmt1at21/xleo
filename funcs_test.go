package xltmpl

import (
	"reflect"
	"testing"
)

func TestCurrentRowAndCol(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{currentRow}}", "{{currentCol}}"},
		{"{{currentRow}}", "{{currentCol}}"},
	})
	out := renderToBytes(t, path, nil)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{{"1", "1", "2"}, {"2", "1", "2"}}
	// Each row has two cells: A = currentRow, B = currentCol (= 2).
	got := [][]string{
		{rows[0][0], rows[0][1]},
		{rows[1][0], rows[1][1]},
	}
	if got[0][0] != "1" || got[0][1] != "2" || got[1][0] != "2" || got[1][1] != "2" {
		t.Errorf("got %v, want rows starting [1,2] then [2,2]; full row 1 = %v want %v", got, rows, want)
	}
}

func TestIndexInLoop(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{range .Items}}"},
		{"{{index1}}", "{{.Name}}"},
		{"{{end}}"},
	})
	data := map[string]any{
		"Items": []map[string]any{
			{"Name": "A"}, {"Name": "B"}, {"Name": "C"},
		},
	}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{{"1", "A"}, {"2", "B"}, {"3", "C"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got %v, want %v", rows, want)
	}
}

func TestUnixTimeFunction(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{`{{unixTime .ts "Asia/Ho_Chi_Minh" "2006-01-02 15:04"}}`},
	})
	// 1700000000 = 2023-11-14 22:13:20 UTC = 2023-11-15 05:13 Asia/Ho_Chi_Minh
	data := map[string]any{"ts": int64(1700000000)}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	if len(rows) != 1 || rows[0][0] != "2023-11-15 05:13" {
		t.Errorf("got %v, want [[2023-11-15 05:13]]", rows)
	}
}

func TestUnixTimeWithDefaults(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{`{{unixTime .ts}}`},
	})
	// 2026-01-01 00:00:00 UTC
	data := map[string]any{"ts": int64(1767225600)}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	if len(rows) != 1 || rows[0][0] != "2026-01-01 00:00:00" {
		t.Errorf("got %v", rows)
	}
}

func TestMathFunctions(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{add .a .b}}", "{{mul .a .b}}", "{{sub .a .b}}", "{{div .a .b}}"},
	})
	data := map[string]any{"a": 10, "b": 4}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	// add=14 (int), mul=40 (int), sub=6 (int), div=2.5 (float)
	if rows[0][0] != "14" || rows[0][1] != "40" || rows[0][2] != "6" || rows[0][3] != "2.5" {
		t.Errorf("got %v", rows)
	}
}

func TestUpperLowerConcat(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{`{{upper .name}}`, `{{lower .name}}`, `{{concat .name " - " .id}}`},
	})
	data := map[string]any{"name": "Hello", "id": 42}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	if rows[0][0] != "HELLO" || rows[0][1] != "hello" || rows[0][2] != "Hello - 42" {
		t.Errorf("got %v", rows)
	}
}

func TestDefaultFunction(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{`{{default .name "(no name)"}}`, `{{default .name "(no name)"}}`},
	})
	data := map[string]any{"name": ""}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	if rows[0][0] != "(no name)" || rows[0][1] != "(no name)" {
		t.Errorf("got %v", rows)
	}
}

func TestNumericLiteralArg(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{add .x 100}}"},
	})
	data := map[string]any{"x": 23}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	if rows[0][0] != "123" {
		t.Errorf("got %v", rows)
	}
}

func TestRegisterCustomFunc(t *testing.T) {
	err := RegisterFunc("double", func(args []any) (any, error) {
		n, _ := args[0].(int)
		return n * 2, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer delete(funcRegistry, "double")

	path := buildTemplate(t, [][]string{
		{"{{double .x}}"},
	})
	out := renderToBytes(t, path, map[string]any{"x": 7})
	rows := readOutput(t, out, "Sheet1")
	if rows[0][0] != "14" {
		t.Errorf("got %v", rows)
	}
}

func TestSprintfFunction(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{`{{sprintf "%05d" .id}}`},
	})
	out := renderToBytes(t, path, map[string]any{"id": 42})
	rows := readOutput(t, out, "Sheet1")
	if rows[0][0] != "00042" {
		t.Errorf("got %v", rows)
	}
}

func TestColNameFunction(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{colName}}", "{{colName}}", "{{colName}}"},
	})
	out := renderToBytes(t, path, nil)
	rows := readOutput(t, out, "Sheet1")
	if !reflect.DeepEqual(rows[0], []string{"A", "B", "C"}) {
		t.Errorf("got %v", rows[0])
	}
}

func TestIndexAndCurrentRowCombined(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"No.", "Name", "Row"},
		{"{{range .Items}}"},
		{"{{index1}}", "{{.}}", "{{currentRow}}"},
		{"{{end}}"},
	})
	data := map[string]any{"Items": []any{"A", "B", "C"}}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{
		{"No.", "Name", "Row"},
		{"1", "A", "2"},
		{"2", "B", "3"},
		{"3", "C", "4"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got %v, want %v", rows, want)
	}
}

func TestUnknownFunctionError(t *testing.T) {
	path := buildTemplate(t, [][]string{{"{{notAFunc .x}}"}})
	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for unknown function")
	}
}
