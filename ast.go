package xltmpl

// node is one element of a sheet's compiled template AST.
// Each sheet compiles into a body of nodes that the renderer walks in order.
type node interface{ nodeTag() }

// staticRow is a template row that is not itself a loop marker.
// Its cells may contain literals or expressions — both are stored as cellTmpl.
type staticRow struct {
	row   int        // 1-based row index in the template
	cells []cellTmpl // cells that have content (empty cells are dropped)
}

// rangeBlock represents a vertical loop (each item -> N rendered rows).
type rangeBlock struct {
	path     []string // path to the slice in the data context (e.g. ["Items"])
	startRow int      // row that contains the {{range}} marker
	endRow   int      // row that contains the {{end}} marker
	body     []node   // nodes between the markers (staticRow or nested rangeBlock)
	// merges holds merge ranges that fall entirely inside this range's body
	// (and are not claimed by a nested range). They are re-applied per
	// iteration using the row offset relative to the body's first row.
	merges []mergeInfo
}

// hrangeBlock represents a horizontal loop inside a single staticRow.
// When the renderer encounters an hrange cell, the cells between {{hrange}}
// and {{hend}} are repeated for every element. To keep the implementation
// fast and simple, an hrange must start and end on the same row.
type hrangeBlock struct {
	path     []string
	template []cellTmpl // the cell template repeated for each element
}

// cellTmpl describes one template cell: position, segments, style.
type cellTmpl struct {
	col      int   // 1-based column index in the template
	styleID  int   // template style id (cloned to the output workbook on demand)
	segments []seg // segments forming the cell value; nil when hrange != nil
	hrange   *hrangeBlock
}

// seg is one slice of a cell's value: literal text or an expression.
type seg struct {
	isExpr bool
	text   string   // used when isExpr is false
	expr   exprNode // used when isExpr is true — a path, call, or literal already compiled
}

func (staticRow) nodeTag()  {}
func (rangeBlock) nodeTag() {}

// sheetTmpl is the compiled template for one worksheet.
type sheetTmpl struct {
	name       string
	cols       []colInfo   // column width configuration
	merges     []mergeInfo // original merge ranges (only re-applied to fully-static regions)
	rowHeights map[int]float64
	body       []node
	maxCol     int
}

type colInfo struct {
	min, max int
	width    float64
}

type mergeInfo struct {
	startRow, startCol int
	endRow, endCol     int
}
