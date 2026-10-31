package render

import (
	"math"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"
)

// autofit は折り返しのある文字が隠れないよう、行の高さを内容に合わせて広げる。
// Excel は開いたときに自動で高さを合わせないため、出力時に見積もって設定する。
func autofit(f *excelize.File, sheet string) {
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return
	}
	merges, _ := f.GetMergeCells(sheet)
	type span struct {
		width float64
		rows  int
	}
	mergeAt := map[string]span{}
	covered := map[string]bool{}
	for _, m := range merges {
		c1, r1, _ := excelize.CellNameToCoordinates(m.GetStartAxis())
		c2, r2, _ := excelize.CellNameToCoordinates(m.GetEndAxis())
		w := 0.0
		for c := c1; c <= c2; c++ {
			w += colWidth(f, sheet, c)
		}
		mergeAt[m.GetStartAxis()] = span{w, r2 - r1 + 1}
		for r := r1; r <= r2; r++ {
			for c := c1; c <= c2; c++ {
				n, _ := excelize.CoordinatesToCellName(c, r)
				if n != m.GetStartAxis() {
					covered[n] = true
				}
			}
		}
	}
	for ri, row := range rows {
		r := ri + 1
		need := 0.0
		for ci, v := range row {
			if v == "" {
				continue
			}
			name, _ := excelize.CoordinatesToCellName(ci+1, r)
			if covered[name] {
				continue
			}
			w := colWidth(f, sheet, ci+1)
			nrows := 1
			if s, ok := mergeAt[name]; ok {
				w, nrows = s.width, s.rows
			}
			h := float64(lineCount(v, w))*15 + 4
			h /= float64(nrows)
			if h > need {
				need = h
			}
		}
		if need > 0 {
			cur, _ := f.GetRowHeight(sheet, r)
			if need > cur {
				_ = f.SetRowHeight(sheet, r, math.Min(need, 409))
			}
		}
	}
}

func colWidth(f *excelize.File, sheet string, col int) float64 {
	name, _ := excelize.ColumnNumberToName(col)
	w, err := f.GetColWidth(sheet, name)
	if err != nil || w <= 0 {
		return 9
	}
	return w
}

// lineCount は幅 width（Excel の列幅単位）で折り返したときの行数の見積もり。全角は2として数える。
func lineCount(s string, width float64) int {
	if width < 2 {
		width = 2
	}
	n := 0
	for _, line := range strings.Split(s, "\n") {
		w := 0.0
		for _, r := range line {
			if r > 0x2E80 || unicode.Is(unicode.Han, r) {
				w += 2
			} else {
				w += 1.1
			}
		}
		n += int(math.Max(1, math.Ceil(w/(width-1))))
	}
	return n
}
