package xltmpl

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// styledTemplate builds a template with a specific style applied to A1.
// Returns the temp path plus the style ID inside the source workbook.
func styledTemplate(t *testing.T, cells [][]string, styleA1 *excelize.Style, mergeRanges [][2]string) string {
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
				t.Fatalf("set cell: %v", err)
			}
		}
	}
	if styleA1 != nil {
		sid, err := f.NewStyle(styleA1)
		if err != nil {
			t.Fatalf("new style: %v", err)
		}
		if err := f.SetCellStyle("Sheet1", "A1", "A1", sid); err != nil {
			t.Fatalf("set style: %v", err)
		}
	}
	for _, m := range mergeRanges {
		if err := f.MergeCell("Sheet1", m[0], m[1]); err != nil {
			t.Fatalf("merge %s..%s: %v", m[0], m[1], err)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tpl.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	return path
}

// TestPreserveCellStyle confirms that fill color, font color, and bold are
// carried over from the template to the output workbook.
func TestPreserveCellStyle(t *testing.T) {
	style := &excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"4472C4"}},
	}
	path := styledTemplate(t, [][]string{{"{{.Title}}"}}, style, nil)

	tpl, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tpl.Render(&buf, map[string]any{"Title": "Hello"}); err != nil {
		t.Fatal(err)
	}

	out, _ := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	defer out.Close()
	sid, err := out.GetCellStyle("Sheet1", "A1")
	if err != nil || sid == 0 {
		t.Fatalf("expected non-default style on A1, got sid=%d err=%v", sid, err)
	}
	got, err := out.GetStyle(sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Font == nil || !got.Font.Bold {
		t.Errorf("font lost: %+v", got.Font)
	}
	if got.Font == nil || got.Font.Color != "FFFFFF" {
		t.Errorf("font color lost: %+v", got.Font)
	}
	if len(got.Fill.Color) == 0 || got.Fill.Color[0] != "4472C4" {
		t.Errorf("fill color lost: %+v", got.Fill)
	}
}

// TestPreserveStaticMerge verifies that a merge entirely outside any range
// is preserved in the output.
func TestPreserveStaticMerge(t *testing.T) {
	path := styledTemplate(t,
		[][]string{
			{"{{.Title}}", "", ""},
			{"Data"},
		},
		nil,
		[][2]string{{"A1", "C1"}}, // merge A1:C1
	)
	tpl, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tpl.Render(&buf, map[string]any{"Title": "Header"}); err != nil {
		t.Fatal(err)
	}
	out, _ := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	defer out.Close()
	merges, _ := out.GetMergeCells("Sheet1")
	found := false
	for _, m := range merges {
		if m.GetStartAxis() == "A1" && m.GetEndAxis() == "C1" {
			found = true
		}
	}
	if !found {
		t.Errorf("static merge A1:C1 lost, got merges = %v", merges)
	}
}

// TestPreserveMultiRowMergeInsideRange checks that a merge spanning multiple
// body rows (e.g. A2:A3 across both rows of the body) is replicated correctly
// for each iteration.
func TestPreserveMultiRowMergeInsideRange(t *testing.T) {
	// Template:
	//   row 1: range
	//   row 2: cell A2 spans down to A3 (A2:A3 merged); B2 = .Name
	//   row 3: B3 = .Detail
	//   row 4: end
	path := styledTemplate(t,
		[][]string{
			{"{{range .Items}}"},
			{"{{.Name}}", "{{.Name}}"},
			{"", "{{.Detail}}"},
			{"{{end}}"},
		},
		nil,
		[][2]string{{"A2", "A3"}},
	)
	tpl, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := map[string]any{"Items": []map[string]any{
		{"Name": "Item1", "Detail": "d1"},
		{"Name": "Item2", "Detail": "d2"},
	}}
	if err := tpl.Render(&buf, data); err != nil {
		t.Fatal(err)
	}
	out, _ := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	defer out.Close()
	merges, _ := out.GetMergeCells("Sheet1")
	// Each iteration emits 2 output rows -> iterations produce A1:A2 and A3:A4.
	want := map[string]string{"A1": "A2", "A3": "A4"}
	got := map[string]string{}
	for _, m := range merges {
		got[m.GetStartAxis()] = m.GetEndAxis()
	}
	for s, e := range want {
		if got[s] != e {
			t.Errorf("missing merge %s:%s, got = %v", s, e, got)
		}
	}
}

// TestPreserveMergeInsideRange verifies that a merge inside the body of a
// range loop is replicated for each iteration (shifted to the right output row).
func TestPreserveMergeInsideRange(t *testing.T) {
	// Template:
	//   row 1: range
	//   row 2: A2:B2 merged, value at A2 only
	//   row 3: end
	path := styledTemplate(t,
		[][]string{
			{"{{range .Items}}"},
			{"{{.}}"},
			{"{{end}}"},
		},
		nil,
		[][2]string{{"A2", "B2"}},
	)
	tpl, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tpl.Render(&buf, map[string]any{"Items": []any{"x", "y", "z"}}); err != nil {
		t.Fatal(err)
	}
	out, _ := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	defer out.Close()
	merges, _ := out.GetMergeCells("Sheet1")
	// Expect a merge for each iteration: A1:B1, A2:B2, A3:B3.
	want := map[string]string{"A1": "B1", "A2": "B2", "A3": "B3"}
	got := map[string]string{}
	for _, m := range merges {
		got[m.GetStartAxis()] = m.GetEndAxis()
	}
	for s, e := range want {
		if got[s] != e {
			t.Errorf("missing merge %s:%s, got merges = %v", s, e, got)
		}
	}
}
