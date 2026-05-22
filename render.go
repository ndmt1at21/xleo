package xltmpl

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Render writes the rendered workbook to w (.xlsx).
//
// data may be:
//   - map[string]any         : the most common form
//   - struct or *struct      : fields are read by name
//   - any other value: only accessible via "." (the value itself)
//
// Render writes sequentially via excelize's StreamWriter for maximum throughput
// and low memory footprint when emitting large workbooks.
func (t *Template) Render(w io.Writer, data any) error {
	out := excelize.NewFile()
	defer out.Close()

	// Rename the default "Sheet1" to the first template sheet.
	defaultSheet := out.GetSheetName(0)

	for i, st := range t.sheets {
		if i == 0 {
			out.SetSheetName(defaultSheet, st.name)
		} else {
			if _, err := out.NewSheet(st.name); err != nil {
				return fmt.Errorf("xltmpl: create sheet %q: %w", st.name, err)
			}
		}
		if err := renderSheet(out, st, data, t); err != nil {
			return fmt.Errorf("xltmpl: render sheet %q: %w", st.name, err)
		}
	}

	if _, err := out.WriteTo(w); err != nil {
		return fmt.Errorf("xltmpl: write output: %w", err)
	}
	return nil
}

// RenderToFile renders the template with the given data and writes the result
// to the specified file path.
func (t *Template) RenderToFile(path string, data any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Render(f, data)
}

func renderSheet(out *excelize.File, st *sheetTmpl, data any, tpl *Template) error {
	sw, err := out.NewStreamWriter(st.name)
	if err != nil {
		return err
	}

	// Apply column widths via the StreamWriter.
	for _, c := range st.cols {
		if err := sw.SetColWidth(c.min, c.max, c.width); err != nil {
			return err
		}
	}

	ctx := &renderCtx{
		out:      out,
		sw:       sw,
		eval:     evalCtx{root: data},
		styleMap: map[int]int{},
		st:       st,
		tpl:      tpl,
	}

	if err := renderNodes(ctx, st.body); err != nil {
		return err
	}

	// Re-apply merge ranges that fall entirely outside any range/hrange block.
	// Merges that overlap a loop body are skipped to avoid incorrect placement.
	if err := applyStaticMerges(ctx); err != nil {
		return err
	}

	return sw.Flush()
}

type renderCtx struct {
	out      *excelize.File
	sw       *excelize.StreamWriter
	eval     evalCtx
	styleMap map[int]int
	outRow   int
	outCol   int   // the column currently being filled (set inside writeStaticRow)
	loopIdx  []int // stack of loop indices for nested range loops
	st       *sheetTmpl
	tpl      *Template
	// expandedRanges records the ranges seen so far (used by the merge logic
	// to detect overlapping merge regions).
	expandedRanges []rangeBlock

	// Buffers reused across rows to reduce allocations on the hot path.
	bufOutCells []outCell
	bufValues   []any
	bufSB       strings.Builder
}

type outCell struct {
	col   int
	value any
	style int
}

func renderNodes(ctx *renderCtx, nodes []node) error {
	for _, n := range nodes {
		switch v := n.(type) {
		case *staticRow:
			if err := writeStaticRow(ctx, v); err != nil {
				return err
			}
		case *rangeBlock:
			if err := renderRange(ctx, v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("xltmpl: unknown node type: %T", n)
		}
	}
	return nil
}

func renderRange(ctx *renderCtx, rb *rangeBlock) error {
	val, err := ctx.eval.resolve(rb.path)
	if err != nil {
		return err
	}
	items, err := asSlice(val)
	if err != nil {
		return err
	}
	ctx.expandedRanges = append(ctx.expandedRanges, *rb)
	prevCur := ctx.eval.cur
	// Push a loop-index slot for this range.
	ctx.loopIdx = append(ctx.loopIdx, 0)
	idxSlot := len(ctx.loopIdx) - 1
	bodyStart := rb.startRow + 1
	for i, it := range items {
		ctx.loopIdx[idxSlot] = i
		ctx.eval.cur = it
		// Remember where this iteration's body begins in the output sheet
		// so we can shift merges accordingly after the body is rendered.
		outRowBefore := ctx.outRow
		if err := renderNodes(ctx, rb.body); err != nil {
			ctx.eval.cur = prevCur
			ctx.loopIdx = ctx.loopIdx[:idxSlot]
			return err
		}
		if err := applyIterationMerges(ctx, rb, bodyStart, outRowBefore); err != nil {
			ctx.eval.cur = prevCur
			ctx.loopIdx = ctx.loopIdx[:idxSlot]
			return err
		}
	}
	ctx.eval.cur = prevCur
	ctx.loopIdx = ctx.loopIdx[:idxSlot]
	return nil
}

// applyIterationMerges re-applies the merges that belong to rb.body for one
// iteration. Row offsets are relative to bodyStart (the template row right
// after the {{range}} marker), and outRowBefore is the output row index
// before this iteration's body started writing.
func applyIterationMerges(ctx *renderCtx, rb *rangeBlock, bodyStart, outRowBefore int) error {
	for _, m := range rb.merges {
		startOff := m.startRow - bodyStart
		endOff := m.endRow - bodyStart
		outStart := outRowBefore + 1 + startOff
		outEnd := outRowBefore + 1 + endOff
		startCell, _ := excelize.CoordinatesToCellName(m.startCol, outStart)
		endCell, _ := excelize.CoordinatesToCellName(m.endCol, outEnd)
		if err := ctx.sw.MergeCell(startCell, endCell); err != nil {
			return err
		}
	}
	return nil
}

func writeStaticRow(ctx *renderCtx, sr *staticRow) error {
	ctx.outRow++
	if len(sr.cells) == 0 {
		// Empty row — keep the original row height if one was set.
		if h, ok := ctx.st.rowHeights[sr.row]; ok && h > 0 {
			return ctx.sw.SetRow(rowAxis(ctx.outRow), []any{}, excelize.RowOpts{Height: h})
		}
		return nil
	}

	// Build the output cells. An hrange shifts subsequent columns, so output
	// column indices are tracked separately from template columns. The
	// outCells buffer is reused across rows to keep the hot path allocation-free.
	startCol := sr.cells[0].col
	outCells := ctx.bufOutCells[:0]
	curOutCol := startCol
	for _, c := range sr.cells {
		// Walk forward by the same column gap that the template had between
		// the previous cell and this one.
		if len(outCells) > 0 {
			prev := sr.cells[len(outCells)-1]
			curOutCol += c.col - prev.col
		}

		if c.hrange != nil {
			items, err := evalSlice(ctx, c.hrange.path)
			if err != nil {
				return err
			}
			prevCur := ctx.eval.cur
			ctx.loopIdx = append(ctx.loopIdx, 0)
			idxSlot := len(ctx.loopIdx) - 1
			startHrCol := curOutCol
			for i, it := range items {
				ctx.loopIdx[idxSlot] = i
				ctx.eval.cur = it
				for j, tpl := range c.hrange.template {
					ctx.outCol = startHrCol + j
					val, err := renderCellValue(ctx, tpl.segments)
					if err != nil {
						ctx.eval.cur = prevCur
						ctx.loopIdx = ctx.loopIdx[:idxSlot]
						return err
					}
					sid := ctx.mapStyle(tpl.styleID)
					outCells = append(outCells, outCell{col: startHrCol + j, value: val, style: sid})
				}
				startHrCol += len(c.hrange.template)
			}
			ctx.eval.cur = prevCur
			ctx.loopIdx = ctx.loopIdx[:idxSlot]
			// Move curOutCol to the last column emitted; the next template cell
			// will then add its own gap on top.
			curOutCol = startHrCol - 1
			continue
		}

		ctx.outCol = curOutCol
		val, err := renderCellValue(ctx, c.segments)
		if err != nil {
			return err
		}
		sid := ctx.mapStyle(c.styleID)
		outCells = append(outCells, outCell{col: curOutCol, value: val, style: sid})
	}

	// Pack the cells into a contiguous values slice from minCol to maxCol.
	minCol := outCells[0].col
	maxCol := minCol
	for _, oc := range outCells {
		if oc.col < minCol {
			minCol = oc.col
		}
		if oc.col > maxCol {
			maxCol = oc.col
		}
	}
	width := maxCol - minCol + 1
	if cap(ctx.bufValues) >= width {
		ctx.bufValues = ctx.bufValues[:width]
		for i := range ctx.bufValues {
			ctx.bufValues[i] = nil
		}
	} else {
		ctx.bufValues = make([]any, width)
	}
	values := ctx.bufValues
	for _, oc := range outCells {
		idx := oc.col - minCol
		if oc.style != 0 {
			values[idx] = excelize.Cell{Value: oc.value, StyleID: oc.style}
		} else {
			values[idx] = oc.value
		}
	}

	startCellName, err := excelize.CoordinatesToCellName(minCol, ctx.outRow)
	if err != nil {
		return err
	}
	opts := []excelize.RowOpts{}
	if h, ok := ctx.st.rowHeights[sr.row]; ok && h > 0 {
		opts = append(opts, excelize.RowOpts{Height: h})
	}
	var writeErr error
	if len(opts) > 0 {
		writeErr = ctx.sw.SetRow(startCellName, values, opts...)
	} else {
		writeErr = ctx.sw.SetRow(startCellName, values)
	}
	// Keep the (possibly grown) slice for the next row.
	ctx.bufOutCells = outCells[:0]
	return writeErr
}

func rowAxis(row int) string {
	return "A" + strconv.Itoa(row)
}

func renderCellValue(ctx *renderCtx, segs []seg) (any, error) {
	if len(segs) == 0 {
		return nil, nil
	}
	// Single-segment expression: return the raw value (preserves number/bool/time type).
	if len(segs) == 1 {
		s := segs[0]
		if s.isExpr {
			v, err := s.expr.eval(ctx)
			if err != nil {
				return nil, err
			}
			return convertValue(v), nil
		}
		return s.text, nil
	}
	// Mixed literal + expression -> concatenate into a string using the shared builder.
	ctx.bufSB.Reset()
	for _, s := range segs {
		if s.isExpr {
			v, err := s.expr.eval(ctx)
			if err != nil {
				return nil, err
			}
			writeAsText(&ctx.bufSB, v)
		} else {
			ctx.bufSB.WriteString(s.text)
		}
	}
	return ctx.bufSB.String(), nil
}

// convertValue preserves the primitive type so that excelize can pick the
// appropriate cell type. time.Time values keep their type for date formatting.
func convertValue(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case time.Time:
		return x
	default:
		return v
	}
}

func writeAsText(b *strings.Builder, v any) {
	if v == nil {
		return
	}
	switch x := v.(type) {
	case string:
		b.WriteString(x)
	case time.Time:
		b.WriteString(x.Format(time.RFC3339))
	default:
		fmt.Fprintf(b, "%v", x)
	}
}

func evalSlice(ctx *renderCtx, path []string) ([]any, error) {
	v, err := ctx.eval.resolve(path)
	if err != nil {
		return nil, err
	}
	return asSlice(v)
}

// mapStyle maps a template styleID to the corresponding styleID on the output
// workbook, caching the result so NewStyle is only called once per style.
func (c *renderCtx) mapStyle(tplStyleID int) int {
	if tplStyleID == 0 {
		return 0
	}
	if out, ok := c.styleMap[tplStyleID]; ok {
		return out
	}
	def := c.tpl.styleDefs[tplStyleID]
	if def == nil {
		c.styleMap[tplStyleID] = 0
		return 0
	}
	outID, err := c.out.NewStyle(def)
	if err != nil {
		c.styleMap[tplStyleID] = 0
		return 0
	}
	c.styleMap[tplStyleID] = outID
	return outID
}

// applyStaticMerges re-applies merge ranges that do not overlap any
// range/hrange block. Merges that intersect a loop body are skipped.
func applyStaticMerges(ctx *renderCtx) error {
	if len(ctx.st.merges) == 0 {
		return nil
	}
	for _, m := range ctx.st.merges {
		if intersectsAnyRange(ctx, m) {
			continue
		}
		offset := totalOffsetBefore(ctx, m.startRow)
		startCell, _ := excelize.CoordinatesToCellName(m.startCol, m.startRow+offset)
		endCell, _ := excelize.CoordinatesToCellName(m.endCol, m.endRow+offset)
		if err := ctx.sw.MergeCell(startCell, endCell); err != nil {
			return err
		}
	}
	return nil
}

func intersectsAnyRange(ctx *renderCtx, m mergeInfo) bool {
	for _, r := range ctx.expandedRanges {
		if !(m.endRow < r.startRow || m.startRow > r.endRow) {
			return true
		}
	}
	return false
}

// totalOffsetBefore would compute the extra row count added by range expansion
// before the given row. The current release only applies merges that sit
// before any range, so this always returns 0.
func totalOffsetBefore(_ *renderCtx, _ int) int {
	return 0
}
