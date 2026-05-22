package xltmpl

import (
	"bytes"
	"io"
	"strconv"
	"testing"
	"time"
)

// BenchmarkRender200K measures render time for 200,000 data rows expanded
// through a range block. Target: under 1 second on a typical dev machine.
//
// Run: go test -bench=BenchmarkRender200K -benchmem -run=^$
func BenchmarkRender200K(b *testing.B) {
	runRenderBenchmark(b, 200_000)
}

func BenchmarkRender50K(b *testing.B) {
	runRenderBenchmark(b, 50_000)
}

func BenchmarkRender10K(b *testing.B) {
	runRenderBenchmark(b, 10_000)
}

func runRenderBenchmark(b *testing.B, n int) {
	tplPath := buildBenchTemplate(b)
	tpl, err := ParseFile(tplPath)
	if err != nil {
		b.Fatal(err)
	}

	items := make([]map[string]any, n)
	now := time.Now()
	for i := range items {
		items[i] = map[string]any{
			"ID":    i + 1,
			"Name":  "Product " + strconv.Itoa(i+1),
			"Qty":   i % 100,
			"Price": float64(i) * 1.5,
			"Date":  now,
		}
	}
	data := map[string]any{
		"Title": "Sales report",
		"Items": items,
		"Total": n,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := tpl.Render(io.Discard, data); err != nil {
			b.Fatal(err)
		}
	}
}

// buildBenchTemplate produces the shared benchmark template.
func buildBenchTemplate(b *testing.B) string {
	b.Helper()
	return buildTemplate(b, [][]string{
		{"{{.Title}}"},
		{"ID", "Product name", "Qty", "Unit price", "Date"},
		{"{{range .Items}}"},
		{"{{.ID}}", "{{.Name}}", "{{.Qty}}", "{{.Price}}", "{{.Date}}"},
		{"{{end}}"},
		{"Total:", "", "", "", "{{.Total}}"},
	})
}

// TestPerfTarget is an acceptance test: one render of 200K rows must finish
// in under 1 second. It FAILS the suite if the threshold is exceeded.
func TestPerfTarget(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	tplPath := buildTemplate(t, [][]string{
		{"{{.Title}}"},
		{"ID", "Product name", "Qty", "Unit price"},
		{"{{range .Items}}"},
		{"{{.ID}}", "{{.Name}}", "{{.Qty}}", "{{.Price}}"},
		{"{{end}}"},
	})
	tpl, err := ParseFile(tplPath)
	if err != nil {
		t.Fatal(err)
	}
	const N = 200_000
	items := make([]map[string]any, N)
	for i := range items {
		items[i] = map[string]any{
			"ID":    i + 1,
			"Name":  "Item " + strconv.Itoa(i+1),
			"Qty":   i % 100,
			"Price": float64(i) * 1.5,
		}
	}
	data := map[string]any{"Title": "Report", "Items": items}

	var buf bytes.Buffer
	buf.Grow(50 * 1024 * 1024)

	start := time.Now()
	if err := tpl.Render(&buf, data); err != nil {
		t.Fatal(err)
	}
	dur := time.Since(start)
	t.Logf("rendered %d rows in %v (file size = %d bytes)", N, dur, buf.Len())
	if dur > time.Second {
		t.Errorf("PERFORMANCE TARGET MISSED: render took %v (target < 1s)", dur)
	}
}
