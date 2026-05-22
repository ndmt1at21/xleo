// Example: build a template in code, then render it with sample data.
//
// Run:
//
//	cd examples/quickstart && go run .
package main

import (
	"fmt"
	"log"
	"os"

	xltmpl "github.com/ndmt1at21/xleo"
	"github.com/xuri/excelize/v2"
)

func main() {
	tplPath := "template.xlsx"
	if err := buildSampleTemplate(tplPath); err != nil {
		log.Fatal(err)
	}
	defer os.Remove(tplPath)

	tpl, err := xltmpl.ParseFile(tplPath)
	if err != nil {
		log.Fatal(err)
	}

	data := map[string]any{
		"Title":    "Sales report",
		"ExportTs": int64(1700000000), // formatted by the template
		"Currency": "USD",
		"Months":   []any{"Jan", "Feb", "Mar"},
		"Items": []map[string]any{
			{"ID": "A001", "Name": "T-shirt", "Qty": 100, "Price": 1000},
			{"ID": "A002", "Name": "Jeans", "Qty": 50, "Price": 2400},
			{"ID": "A003", "Name": "Sneakers", "Qty": 30, "Price": 4800},
		},
		"Total": 780000,
	}

	if err := tpl.RenderToFile("output.xlsx", data); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Wrote output.xlsx")
}

func buildSampleTemplate(path string) error {
	f := excelize.NewFile()
	defer f.Close()
	cells := [][]string{
		{"{{upper .Title}}"},
		{"Exported at:", `{{unixTime .ExportTs "Asia/Ho_Chi_Minh" "02/01/2006 15:04"}}`},
		{},
		{"No.", "Item", "{{hrange .Months}}", "{{.}}", "{{hend}}"},
		{"{{range .Items}}"},
		{"{{index1}}", "{{.Name}}", "{{.ID}}", "{{.Qty}}", `{{sprintf "%d" .Price}} {{$.Currency}}`},
		{"{{end}}"},
		{"Total:", "", "", "", "{{.Total}} {{.Currency}}"},
	}
	for r, row := range cells {
		for c, v := range row {
			if v == "" {
				continue
			}
			name, _ := excelize.CoordinatesToCellName(c+1, r+1)
			if err := f.SetCellValue("Sheet1", name, v); err != nil {
				return err
			}
		}
	}
	return f.SaveAs(path)
}
