package xltmpl

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// Template is a pre-compiled Excel template. It can be reused safely for many
// renders with different data (it is not mutated after compilation).
type Template struct {
	sheets []*sheetTmpl
	// styleDefs maps each original styleID in the template to its captured
	// definition. Styles are read up front because the source workbook is
	// closed before render time.
	styleDefs map[int]*excelize.Style
}

// ParseFile reads an Excel template file and compiles it into a Template.
func ParseFile(path string) (*Template, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("xltmpl: open template file: %w", err)
	}
	defer f.Close()
	return parseWorkbook(f)
}

// ParseBytes compiles a template from a byte slice (.xlsx contents).
func ParseBytes(data []byte) (*Template, error) {
	r := bytesReader(data)
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("xltmpl: read template bytes: %w", err)
	}
	defer f.Close()
	return parseWorkbook(f)
}

func bytesReader(b []byte) *byteReadSeeker { return &byteReadSeeker{b: b} }

type byteReadSeeker struct {
	b   []byte
	off int64
}

func (r *byteReadSeeker) Read(p []byte) (int, error) {
	if r.off >= int64(len(r.b)) {
		return 0, errEOF
	}
	n := copy(p, r.b[r.off:])
	r.off += int64(n)
	return n, nil
}

func (r *byteReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case 0:
		abs = offset
	case 1:
		abs = r.off + offset
	case 2:
		abs = int64(len(r.b)) + offset
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative seek")
	}
	r.off = abs
	return abs, nil
}

// errEOF avoids importing io just for this sentinel.
var errEOF = fmt.Errorf("EOF")

func parseWorkbook(f *excelize.File) (*Template, error) {
	tpl := &Template{styleDefs: map[int]*excelize.Style{}}
	for _, sheet := range f.GetSheetList() {
		st, err := parseSheet(f, sheet, tpl)
		if err != nil {
			return nil, fmt.Errorf("xltmpl: sheet %q: %w", sheet, err)
		}
		tpl.sheets = append(tpl.sheets, st)
	}
	return tpl, nil
}

// captureStyle reads a style definition once per styleID and stores it.
func (t *Template) captureStyle(f *excelize.File, id int) {
	if id == 0 {
		return
	}
	if _, ok := t.styleDefs[id]; ok {
		return
	}
	st, err := f.GetStyle(id)
	if err != nil || st == nil {
		t.styleDefs[id] = nil
		return
	}
	t.styleDefs[id] = st
}

// parseSheet reads all cells in a sheet, builds a per-row structure, and then
// folds them into range/hrange blocks.
func parseSheet(f *excelize.File, sheet string, tpl *Template) (*sheetTmpl, error) {
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, err
	}
	st := &sheetTmpl{name: sheet, rowHeights: map[int]float64{}}

	// Column widths.
	cols, _ := f.GetCols(sheet)
	for i := range cols {
		w, err := f.GetColWidth(sheet, indexToColName(i+1))
		if err == nil && w > 0 {
			st.cols = append(st.cols, colInfo{min: i + 1, max: i + 1, width: w})
		}
	}

	// Merged ranges.
	merges, _ := f.GetMergeCells(sheet)
	for _, m := range merges {
		sc, sr, err1 := excelize.CellNameToCoordinates(m.GetStartAxis())
		ec, er, err2 := excelize.CellNameToCoordinates(m.GetEndAxis())
		if err1 == nil && err2 == nil {
			st.merges = append(st.merges, mergeInfo{startRow: sr, startCol: sc, endRow: er, endCol: ec})
		}
	}

	// Row heights.
	for i := range rows {
		h, err := f.GetRowHeight(sheet, i+1)
		if err == nil && h > 0 {
			st.rowHeights[i+1] = h
		}
	}

	// Parse each row — collect cells or recognise a marker.
	type rowEntry struct {
		row    int
		cells  []cellTmpl
		marker markerType
		mpath  []string
	}
	entries := make([]rowEntry, 0, len(rows))

	for rIdx, row := range rows {
		rowNum := rIdx + 1
		var cells []cellTmpl
		var rowMarker markerType
		var rowPath []string
		var rowMaxCol int

		// Track an hrange in progress on the same row.
		hrActive := false
		var hrPath []string
		var hrCells []cellTmpl
		hrStartCol := 0

		for cIdx, raw := range row {
			colNum := cIdx + 1
			if raw == "" {
				continue
			}
			if colNum > rowMaxCol {
				rowMaxCol = colNum
			}
			segs, mk, mpath, err := parseCell(raw)
			if err != nil {
				return nil, fmt.Errorf("cell %s%d: %w", indexToColName(colNum), rowNum, err)
			}
			// Capture the original cell style.
			cellName, _ := excelize.CoordinatesToCellName(colNum, rowNum)
			sid, _ := f.GetCellStyle(sheet, cellName)
			tpl.captureStyle(f, sid)

			switch mk {
			case markerRange, markerEnd:
				// Row-level marker: must be the only marker on the row.
				if rowMarker != markerNone {
					return nil, fmt.Errorf("row %d: only one marker is allowed per row", rowNum)
				}
				rowMarker = mk
				rowPath = mpath
			case markerHRange:
				if hrActive {
					return nil, fmt.Errorf("row %d: nested hrange is not supported", rowNum)
				}
				hrActive = true
				hrPath = mpath
				hrCells = hrCells[:0]
				hrStartCol = colNum
			case markerHEnd:
				if !hrActive {
					return nil, fmt.Errorf("row %d: {{hend}} without a matching {{hrange}}", rowNum)
				}
				body := make([]cellTmpl, len(hrCells))
				copy(body, hrCells)
				cells = append(cells, cellTmpl{
					col:    hrStartCol,
					hrange: &hrangeBlock{path: hrPath, template: body},
				})
				hrActive = false
				hrPath = nil
				hrCells = nil
				hrStartCol = 0
			case markerNone:
				ct := cellTmpl{col: colNum, styleID: sid, segments: segs}
				if hrActive {
					hrCells = append(hrCells, ct)
				} else {
					cells = append(cells, ct)
				}
			}
		}

		if hrActive {
			return nil, fmt.Errorf("row %d: missing {{hend}}", rowNum)
		}
		if rowMaxCol > st.maxCol {
			st.maxCol = rowMaxCol
		}
		entries = append(entries, rowEntry{
			row:    rowNum,
			cells:  cells,
			marker: rowMarker,
			mpath:  rowPath,
		})
	}

	// Fold the per-row entries into a tree using a stack for nested ranges.
	type frame struct {
		path     []string
		startRow int
		body     []node
	}
	root := &frame{}
	stack := []*frame{root}

	for _, e := range entries {
		top := stack[len(stack)-1]
		switch e.marker {
		case markerRange:
			stack = append(stack, &frame{path: e.mpath, startRow: e.row})
		case markerEnd:
			if len(stack) <= 1 {
				return nil, fmt.Errorf("row %d: extra {{end}}", e.row)
			}
			done := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			parent := stack[len(stack)-1]
			parent.body = append(parent.body, &rangeBlock{
				path:     done.path,
				startRow: done.startRow,
				endRow:   e.row,
				body:     done.body,
			})
		case markerNone:
			if len(e.cells) == 0 {
				// Empty row — still append to preserve row height.
				top.body = append(top.body, &staticRow{row: e.row})
			} else {
				top.body = append(top.body, &staticRow{row: e.row, cells: e.cells})
			}
		}
	}
	if len(stack) != 1 {
		return nil, fmt.Errorf("missing {{end}} for a range block")
	}
	st.body = root.body

	// Re-classify merges: a merge fully inside a range body (not crossing the
	// {{range}}/{{end}} markers) is attached to the innermost containing
	// range so it can be re-applied per iteration. The remaining merges stay
	// at sheet level and are applied once.
	remaining := st.merges[:0]
	for _, m := range st.merges {
		if rb := innermostContainingRange(st.body, m); rb != nil {
			rb.merges = append(rb.merges, m)
		} else {
			remaining = append(remaining, m)
		}
	}
	st.merges = remaining
	return st, nil
}

// innermostContainingRange walks the node tree and returns the deepest
// rangeBlock whose body fully contains the given merge (i.e. body rows only,
// excluding the range/end marker rows). Returns nil if no range qualifies.
func innermostContainingRange(nodes []node, m mergeInfo) *rangeBlock {
	var best *rangeBlock
	for _, n := range nodes {
		rb, ok := n.(*rangeBlock)
		if !ok {
			continue
		}
		// "Body" rows are strictly between startRow and endRow.
		if m.startRow > rb.startRow && m.endRow < rb.endRow {
			best = rb
			if deeper := innermostContainingRange(rb.body, m); deeper != nil {
				best = deeper
			}
		}
	}
	return best
}

// indexToColName turns a 1-based column index into its letter (A, B, ..., AA, AB, ...).
func indexToColName(idx int) string {
	name, _ := excelize.ColumnNumberToName(idx)
	return name
}
