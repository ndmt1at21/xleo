package xltmpl

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/xuri/excelize/v2"
)

// tHelper is the subset of testing.TB methods used to build templates.
// Using an interface lets the helper run from both TestXxx (*testing.T) and
// BenchmarkXxx (*testing.B).
type tHelper interface {
	Helper()
	Fatalf(format string, args ...any)
	TempDir() string
}

// buildTemplate writes a temporary template .xlsx from a 2D string layout
// and returns its path (the testing framework cleans up the temp dir).
func buildTemplate(t tHelper, cells [][]string) string {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for r, row := range cells {
		for c, v := range row {
			if v == "" {
				continue
			}
			name, _ := excelize.CoordinatesToCellName(c+1, r+1)
			if err := f.SetCellValue("Sheet1", name, v); err != nil {
				t.Fatalf("set cell %s: %v", name, err)
			}
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tpl.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save template: %v", err)
	}
	return path
}

// readOutput reads the rendered .xlsx bytes into a 2D string array for assertions.
func readOutput(t *testing.T, data []byte, sheet string) [][]string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	return rows
}

func renderToBytes(t *testing.T, tplPath string, data any) []byte {
	t.Helper()
	tpl, err := ParseFile(tplPath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := tpl.Render(&buf, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.Bytes()
}

func TestSimpleVariableSubstitution(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"Report", "{{.Title}}"},
		{"Date", "{{.Date}}"},
	})
	data := map[string]any{"Title": "Q1 Sales", "Date": "2026-05-22"}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{
		{"Report", "Q1 Sales"},
		{"Date", "2026-05-22"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got %v, want %v", rows, want)
	}
}

func TestMixedSegments(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"Hello {{.Name}}, you have {{.Count}} orders"},
	})
	data := map[string]any{"Name": "Alice", "Count": 5}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	if len(rows) != 1 || rows[0][0] != "Hello Alice, you have 5 orders" {
		t.Errorf("got %v", rows)
	}
}

// TestUTF8Roundtrip verifies that non-ASCII characters round-trip correctly,
// for users rendering Vietnamese / other multilingual data.
func TestUTF8Roundtrip(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"Xin chào {{.Name}}, bạn có {{.Count}} đơn"},
	})
	data := map[string]any{"Name": "Trí", "Count": 5}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	if len(rows) != 1 || rows[0][0] != "Xin chào Trí, bạn có 5 đơn" {
		t.Errorf("got %v", rows)
	}
}

func TestNumericTypePreserved(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{.X}}"},
	})
	data := map[string]any{"X": 42}
	out := renderToBytes(t, path, data)
	f, _ := excelize.OpenReader(bytes.NewReader(out))
	defer f.Close()
	v, _ := f.GetCellValue("Sheet1", "A1")
	// The number must round-trip as "42" without being quoted as a string.
	if v != "42" {
		t.Errorf("got %q want %q", v, "42")
	}
	// And the cell must not be stored as an inline/shared string.
	ct, _ := f.GetCellType("Sheet1", "A1")
	if ct == excelize.CellTypeInlineString || ct == excelize.CellTypeSharedString {
		t.Errorf("cell type = %v, should not be a string", ct)
	}
}

func TestVerticalRange(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"ID", "Name", "Qty"},
		{"{{range .Items}}"},
		{"{{.ID}}", "{{.Name}}", "{{.Qty}}"},
		{"{{end}}"},
		{"Total:", "", "{{.Total}}"},
	})
	data := map[string]any{
		"Items": []map[string]any{
			{"ID": "A1", "Name": "Shirt", "Qty": 10},
			{"ID": "B2", "Name": "Pen", "Qty": 25},
			{"ID": "C3", "Name": "Cup", "Qty": 7},
		},
		"Total": 42,
	}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{
		{"ID", "Name", "Qty"},
		{"A1", "Shirt", "10"},
		{"B2", "Pen", "25"},
		{"C3", "Cup", "7"},
		{"Total:", "", "42"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("\ngot:  %v\nwant: %v", rows, want)
	}
}

func TestHorizontalRange(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"Item", "{{hrange .Months}}", "{{.}}", "{{hend}}"},
		{"Sales", "{{hrange .Values}}", "{{.}}", "{{hend}}"},
	})
	data := map[string]any{
		"Months": []any{"Jan", "Feb", "Mar"},
		"Values": []any{100, 200, 300},
	}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{
		{"Item", "Jan", "Feb", "Mar"},
		{"Sales", "100", "200", "300"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("\ngot:  %v\nwant: %v", rows, want)
	}
}

func TestNestedRange(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{range .Groups}}"},
		{"Group:", "{{.Name}}"},
		{"{{range .Items}}"},
		{"  -", "{{.}}"},
		{"{{end}}"},
		{"{{end}}"},
	})
	data := map[string]any{
		"Groups": []map[string]any{
			{"Name": "A", "Items": []any{"x", "y"}},
			{"Name": "B", "Items": []any{"z"}},
		},
	}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{
		{"Group:", "A"},
		{"  -", "x"},
		{"  -", "y"},
		{"Group:", "B"},
		{"  -", "z"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("\ngot:  %v\nwant: %v", rows, want)
	}
}

func TestOuterContextAccess(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{range .Items}}"},
		{"{{.Name}}", "{{$.Currency}}"},
		{"{{end}}"},
	})
	data := map[string]any{
		"Currency": "VND",
		"Items": []map[string]any{
			{"Name": "X"}, {"Name": "Y"},
		},
	}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{
		{"X", "VND"},
		{"Y", "VND"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("\ngot:  %v\nwant: %v", rows, want)
	}
}

func TestStructDataSource(t *testing.T) {
	type Item struct {
		ID   string
		Name string
	}
	type Doc struct {
		Title string
		Items []Item
	}
	path := buildTemplate(t, [][]string{
		{"{{.Title}}"},
		{"{{range .Items}}"},
		{"{{.ID}}", "{{.Name}}"},
		{"{{end}}"},
	})
	data := Doc{Title: "Report", Items: []Item{{"1", "A"}, {"2", "B"}}}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{
		{"Report"},
		{"1", "A"},
		{"2", "B"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("\ngot:  %v\nwant: %v", rows, want)
	}
}

func TestEmptyRangeProducesZeroRows(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"Header"},
		{"{{range .Items}}"},
		{"{{.}}"},
		{"{{end}}"},
		{"Footer"},
	})
	data := map[string]any{"Items": []any{}}
	out := renderToBytes(t, path, data)
	rows := readOutput(t, out, "Sheet1")
	want := [][]string{{"Header"}, {"Footer"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("\ngot:  %v\nwant: %v", rows, want)
	}
}

func TestRenderToFile(t *testing.T) {
	path := buildTemplate(t, [][]string{{"{{.X}}"}})
	tpl, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.xlsx")
	if err := tpl.RenderToFile(out, map[string]any{"X": "ok"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	rows := readOutput(t, b, "Sheet1")
	if rows[0][0] != "ok" {
		t.Errorf("got %v", rows)
	}
}

func TestUnclosedRange(t *testing.T) {
	path := buildTemplate(t, [][]string{
		{"{{range .Items}}"},
		{"{{.}}"},
	})
	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for unclosed range")
	}
}

// TestBigRange checks correctness with a large range (correctness only — see benchmarks for speed).
func TestBigRange(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	path := buildTemplate(t, [][]string{
		{"ID", "Value"},
		{"{{range .Items}}"},
		{"{{.ID}}", "{{.V}}"},
		{"{{end}}"},
	})
	items := make([]map[string]any, 1000)
	for i := range items {
		items[i] = map[string]any{"ID": strconv.Itoa(i), "V": i * 2}
	}
	out := renderToBytes(t, path, map[string]any{"Items": items})
	rows := readOutput(t, out, "Sheet1")
	if len(rows) != 1001 {
		t.Fatalf("rows = %d, want 1001", len(rows))
	}
	if rows[1000][0] != "999" || rows[1000][1] != "1998" {
		t.Errorf("last row = %v", rows[1000])
	}
}
