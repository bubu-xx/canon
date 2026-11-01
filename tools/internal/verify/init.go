package verify

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
	"go.yaml.in/yaml/v3"
)

// Init は情報源から照合の雛形（区切りごとに1つの文）を作る。既にあれば force のときだけ上書きする。
// split なら情報源のファイルごとに .canon/verify/<ファイル名>.yaml に分けて作る（並行で照合するとき、書き込みがぶつからない）。
func Init(p *project.Project, force, split bool) (int, int, error) {
	units := Units(p.SourcesDir())
	files := map[string]*File{}
	n, na := 0, 0
	for _, a := range AnchorOrder(units) {
		e := Entry{Anchor: a}
		for i, u := range units[a] {
			e.Statements = append(e.Statements, Statement{Unit: i + 1, Source: u, Text: u})
			n++
		}
		na++
		path := Path(p)
		if split {
			src := filepath.Base(p.Anchors[a])
			path = filepath.Join(Dir(p), strings.TrimSuffix(src, filepath.Ext(src))+".yaml")
		}
		if files[path] == nil {
			files[path] = &File{}
		}
		files[path].Entries = append(files[path].Entries, e)
	}
	for path := range files {
		if _, err := os.Stat(path); err == nil && !force {
			return 0, 0, fmt.Errorf("%s は既にあります（作り直すなら --force）", path)
		}
	}
	if split {
		if err := os.MkdirAll(Dir(p), 0o755); err != nil {
			return 0, 0, err
		}
	}
	for path, f := range files {
		if err := writeFile(path, f); err != nil {
			return 0, 0, err
		}
	}
	return na, n, nil
}

func writeFile(path string, f *File) error {
	var buf bytes.Buffer
	buf.WriteString("# canon verify --init が作った照合の雛形。\n")
	buf.WriteString("# unit と source は書き換えない。1つの区切りに複数の主張があれば、同じ unit・source の文を増やして text で分ける（まとめてはいけない）。\n")
	buf.WriteString("# 各文に scope（in / out）と、in なら verdict・code・note などを書く。書き方は verify スキルを参照。\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

var severity = map[string]int{"": 0, "out": 1, "not_checkable": 2, "consistent": 3, "incomplete": 4, "inconsistent": 5, "not_implemented": 5}

var verdictJA = map[string]string{"": "未記録", "out": "範囲外", "not_checkable": "照合不能", "consistent": "一致", "incomplete": "記述不足", "inconsistent": "矛盾", "not_implemented": "未実装"}

// unitVerdict は区切りごとの最も重い判定。
func unitVerdicts(f *File) map[string]map[int]Statement {
	out := map[string]map[int]Statement{}
	for _, e := range f.Entries {
		a := model.NormalizeRef(e.Anchor)
		if out[a] == nil {
			out[a] = map[int]Statement{}
		}
		for _, s := range e.Statements {
			v := s.Verdict
			if s.Scope == "out" {
				v = "out"
			}
			cur, ok := out[a][s.Unit]
			cv := cur.Verdict
			if cur.Scope == "out" {
				cv = "out"
			}
			if !ok || severity[v] > severity[cv] {
				s.Verdict = v
				out[a][s.Unit] = s
			}
		}
	}
	return out
}

// Compare は2つの照合の記録を区切りごとに突き合わせる。
func Compare(p *project.Project, a, b *File, nameA, nameB string) string {
	units := Units(p.SourcesDir())
	va, vb := unitVerdicts(a), unitVerdicts(b)
	type row struct {
		anchor string
		unit   int
		sa, sb Statement
	}
	var same, diff []row
	var union []row
	total := 0
	for _, an := range AnchorOrder(units) {
		for i := range units[an] {
			total++
			sa, sb := va[an][i+1], vb[an][i+1]
			r := row{an, i + 1, sa, sb}
			if severity[sa.Verdict] >= 5 || severity[sb.Verdict] >= 5 {
				union = append(union, r)
			}
			if sa.Verdict == sb.Verdict {
				same = append(same, r)
			} else {
				diff = append(diff, r)
			}
		}
	}
	var b2 strings.Builder
	fmt.Fprintf(&b2, "区切り %d のうち、判定が同じ %d・違う %d（一致率 %.0f%%）\n", total, len(same), len(diff), 100*float64(len(same))/float64(max(total, 1)))
	count := func(m map[string]map[int]Statement) map[string]int {
		c := map[string]int{}
		for _, an := range AnchorOrder(units) {
			for i := range units[an] {
				c[m[an][i+1].Verdict]++
			}
		}
		return c
	}
	ca, cb := count(va), count(vb)
	keys := []string{"inconsistent", "not_implemented", "incomplete", "consistent", "not_checkable", "out", ""}
	for _, k := range keys {
		if ca[k]+cb[k] > 0 {
			fmt.Fprintf(&b2, "  %s: %s %d / %s %d\n", verdictJA[k], nameA, ca[k], nameB, cb[k])
		}
	}
	both := 0
	for _, r := range union {
		if severity[r.sa.Verdict] >= 5 && severity[r.sb.Verdict] >= 5 {
			both++
		}
	}
	fmt.Fprintf(&b2, "\n■ 矛盾・未実装の和集合 %d 件（両方 %d、片方だけ %d）\n", len(union), both, len(union)-both)
	for _, r := range union {
		mark := "両方"
		if severity[r.sa.Verdict] < 5 {
			mark = nameB + "だけ（" + nameA + ": " + verdictJA[r.sa.Verdict] + "）"
		} else if severity[r.sb.Verdict] < 5 {
			mark = nameA + "だけ（" + nameB + ": " + verdictJA[r.sb.Verdict] + "）"
		}
		fmt.Fprintf(&b2, "  %s #%d「%s」 %s\n", r.anchor, r.unit, short(units[r.anchor][r.unit-1]), mark)
		for _, s := range []Statement{r.sa, r.sb} {
			if severity[s.Verdict] >= 5 && s.Note != "" {
				fmt.Fprintf(&b2, "      %s\n", short80(s.Note))
			}
		}
	}
	sort.Slice(diff, func(i, j int) bool {
		return diff[i].anchor < diff[j].anchor || diff[i].anchor == diff[j].anchor && diff[i].unit < diff[j].unit
	})
	fmt.Fprintf(&b2, "\n■ 判定が違う区切り %d 件\n", len(diff))
	for _, r := range diff {
		fmt.Fprintf(&b2, "  %s #%d「%s」 %s: %s / %s: %s\n", r.anchor, r.unit, short(units[r.anchor][r.unit-1]), nameA, verdictJA[r.sa.Verdict], nameB, verdictJA[r.sb.Verdict])
	}
	return b2.String()
}

func short80(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return string(r)
}
