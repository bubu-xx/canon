package render

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bubu-xx/canon/tools/internal/gaps"
	"github.com/bubu-xx/canon/tools/internal/graph"
	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
	"github.com/xuri/excelize/v2"
	"go.yaml.in/yaml/v3"
)

// TemplateMap はテンプレートのどのセルに何を入れるかの対応表。
type TemplateMap struct {
	Document string     `yaml:"document"` // 文書名（不足一覧の見出し）
	Sheets   []SheetMap `yaml:"sheets"`
}

// Expr は値の式と、その項目の名前（不足一覧に出す）。
// 対応表では "式" だけでも {expr: 式, label: 名前} でも書ける。
type Expr struct {
	Expr  string `yaml:"expr"`
	Label string `yaml:"label"`
}

// UnmarshalYAML は文字列だけの書き方も受け付ける。
func (x *Expr) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		x.Expr = n.Value
		return nil
	}
	type raw Expr
	return n.Decode((*raw)(x))
}

// SheetMap は1シート分の対応。
type SheetMap struct {
	Template string              `yaml:"template"` // テンプレート内のシート名
	Each     string              `yaml:"each"`     // 型。要素1件につきシートを複製する
	Name     string              `yaml:"name"`     // 複製したシートの名前（{id} {name}）
	Where    map[string][]string `yaml:"where"`    // 絞り込み。"fields.level": [unit, "(none)"]
	Cells    map[string]Expr     `yaml:"cells"`    // セル番地 → 式
	Lists    []ListMap           `yaml:"lists"`
	Rows     string              `yaml:"rows"`    // 型 または questions。1件を1行として並べる
	Start    int                 `yaml:"start"`   // 書き始める行
	Columns  map[string]Expr     `yaml:"columns"` // 列 → 式
}

// ListMap はリストの値を下へ1行1項目で展開する指定。
type ListMap struct {
	From string `yaml:"from"`
	At   string `yaml:"at"`
	Rows int    `yaml:"rows"`
}

// Coverage は1項目の充足状況（不足一覧の1行）。
type Coverage struct {
	Sheet     string `json:"sheet"`
	Label     string `json:"label"`
	Expr      string `json:"expr"`
	Kind      string `json:"kind"` // ok / partial / no_type / no_field / no_place / no_elements / blank
	Confirmed int    `json:"confirmed"`
	Total     int    `json:"total"`
	Note      string `json:"note"`
}

type tctx struct {
	f      *excelize.File
	p      *project.Project
	g      *graph.Graph
	filled map[string]int
	cov    map[string]*Coverage
	order  []string
}

const gapPrefix = "gap="

// WriteXLSXTemplate は既存のテンプレートに Canon の値を流し込み、項目ごとの充足状況を返す。
func WriteXLSXTemplate(p *project.Project, templatePath, mapPath, out string) ([]Coverage, string, error) {
	data, err := os.ReadFile(mapPath)
	if err != nil {
		return nil, "", err
	}
	var m TemplateMap
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, "", fmt.Errorf("対応表 %s: %w", mapPath, err)
	}
	f, err := excelize.OpenFile(templatePath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	c := &tctx{f: f, p: p, g: graph.Build(p), filled: map[string]int{}, cov: map[string]*Coverage{}}
	var remove []string
	var touched []string
	for _, sm := range m.Sheets {
		idx, err := f.GetSheetIndex(sm.Template)
		if err != nil || idx < 0 {
			return nil, "", fmt.Errorf("テンプレートにシート %q がありません", sm.Template)
		}
		switch {
		case sm.Each != "":
			els := c.select_(sm.Each, sm.Where)
			for _, e := range els {
				name := sheetName(strings.NewReplacer("{id}", e.ID, "{name}", e.Name).Replace(or(sm.Name, "{id}")))
				ni, err := f.NewSheet(name)
				if err != nil {
					return nil, "", err
				}
				if err := f.CopySheet(idx, ni); err != nil {
					return nil, "", err
				}
				_ = f.MoveSheet(name, sm.Template)
				for _, addr := range sortedKeys(sm.Cells) {
					c.put(sm.Template, name, addr, e, sm.Cells[addr])
				}
				for _, l := range sm.Lists {
					c.list(sm.Template, name, e, l)
				}
				touched = append(touched, name)
			}
			if len(els) == 0 {
				// 要素が無いときは雛形を残し、無いことを書く
				for _, addr := range sortedKeys(sm.Cells) {
					c.record(sm.Template, sm.Cells[addr], c.typeState(sm.Each), 0, 0)
				}
				first := sortedKeys(sm.Cells)
				if len(first) > 0 {
					c.writeNote(sm.Template, first[0], c.typeMessage(sm.Each))
				}
				touched = append(touched, sm.Template)
			} else {
				remove = append(remove, sm.Template)
			}
		case sm.Rows != "":
			if err := c.rows(sm); err != nil {
				return nil, "", err
			}
			touched = append(touched, sm.Template)
		default:
			for _, addr := range sortedKeys(sm.Cells) {
				c.put(sm.Template, sm.Template, addr, nil, sm.Cells[addr])
			}
			touched = append(touched, sm.Template)
		}
	}
	for _, s := range remove {
		_ = f.DeleteSheet(s)
	}
	for _, s := range touched {
		if idx, _ := f.GetSheetIndex(s); idx >= 0 {
			autofit(f, s)
		}
	}
	f.SetActiveSheet(0)
	if err := f.SaveAs(out); err != nil {
		return nil, "", err
	}
	var cov []Coverage
	for _, k := range c.order {
		cov = append(cov, *c.cov[k])
	}
	return cov, or(m.Document, strings.TrimSuffix(templatePath, ".xlsx")), nil
}

// select_ は型と絞り込みに合う要素を Canon の並び順で返す。
func (c *tctx) select_(typ string, where map[string][]string) []*model.Element {
	var out []*model.Element
	for _, id := range c.p.Order {
		e := c.p.Elements[id]
		if e.Type != typ || !match(e, where) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func match(e *model.Element, where map[string][]string) bool {
	for key, allowed := range where {
		fn := strings.TrimPrefix(key, "fields.")
		f := e.Fields[fn]
		v := "(none)"
		if f != nil && f.Status == model.StatusConfirmed {
			v = inline(f.Value)
		}
		ok := false
		for _, a := range allowed {
			if a == v {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (c *tctx) rows(sm SheetMap) error {
	start := sm.Start
	if start <= 0 {
		start = 2
	}
	sheet := sm.Template
	cols := sortedKeys(sm.Columns)
	base := map[string]int{}
	for _, col := range cols {
		base[col], _ = c.f.GetCellStyle(sheet, cell(col, start))
	}
	row := start
	if sm.Rows == "questions" {
		qs := sortedQuestions(c.p)
		for _, q := range qs {
			for _, col := range cols {
				c.write(sheet, cell(col, row), base[col], questionValue(c.p, q, sm.Columns[col].Expr), "")
			}
			row++
		}
		if len(qs) == 0 && len(cols) > 0 {
			c.writeNote(sheet, cell(cols[0], start), "（未回答・回答済みの質問はありません）")
		}
		return nil
	}
	els := c.select_(sm.Rows, sm.Where)
	stat := map[string][2]int{}
	for _, e := range els {
		for _, col := range cols {
			v, status := c.eval(e, sm.Columns[col].Expr)
			c.write(sheet, cell(col, row), base[col], v, status)
			s := stat[col]
			s[1]++
			if status == "" {
				s[0]++
			}
			stat[col] = s
		}
		row++
	}
	for _, col := range cols {
		x := sm.Columns[col]
		if len(els) == 0 {
			c.record(sheet, x, c.typeState(sm.Rows), 0, 0)
			continue
		}
		c.record(sheet, x, c.exprState(sm.Rows, x.Expr), stat[col][0], stat[col][1])
	}
	if len(els) == 0 && len(cols) > 0 {
		c.writeNote(sheet, cell(cols[0], start), c.typeMessage(sm.Rows))
	}
	return nil
}

func (c *tctx) put(tmplSheet, sheet, addr string, e *model.Element, x Expr) {
	style, _ := c.f.GetCellStyle(sheet, addr)
	v, status := c.eval(e, x.Expr)
	c.write(sheet, addr, style, v, status)
	ok := 0
	if status == "" {
		ok = 1
	}
	kind := "ok"
	if e != nil {
		kind = c.exprState(e.Type, x.Expr)
	} else {
		kind = c.exprState("", x.Expr)
	}
	c.record(tmplSheet, x, kind, ok, 1)
}

func (c *tctx) list(tmplSheet, sheet string, e *model.Element, l ListMap) {
	col, row, err := excelize.CellNameToCoordinates(l.At)
	if err != nil {
		return
	}
	name, _ := excelize.ColumnNumberToName(col)
	fn := strings.TrimPrefix(l.From, "fields.")
	fl := e.Fields[fn]
	style, _ := c.f.GetCellStyle(sheet, l.At)
	x := Expr{Expr: l.From}
	if fl == nil || fl.Status != model.StatusConfirmed {
		v, st := c.eval(e, l.From)
		c.write(sheet, l.At, style, v, st)
		c.record(tmplSheet, x, c.exprState(e.Type, l.From), 0, 1)
		return
	}
	var items []string
	if xs, ok := fl.Value.([]any); ok {
		for _, x := range xs {
			items = append(items, inline(x))
		}
	} else {
		items = []string{inline(fl.Value)}
	}
	max := l.Rows
	if max <= 0 {
		max = len(items)
	}
	for i, it := range items {
		if i == max-1 && len(items) > max {
			c.write(sheet, cell(name, row+i), style, fmt.Sprintf("%s（ほか %d 件）", it, len(items)-max), "")
			break
		}
		c.write(sheet, cell(name, row+i), style, it, "")
	}
	c.record(tmplSheet, x, "ok", 1, 1)
}

// eval は式を値にする。戻り値の2番目は色付け用の状態（"" は確定）。
func (c *tctx) eval(e *model.Element, expr string) (string, string) {
	expr = strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(expr, "="):
		return strings.TrimPrefix(expr, "="), ""
	case strings.HasPrefix(expr, gapPrefix):
		return "（Canon にこの情報の置き場がありません：" + strings.TrimPrefix(expr, gapPrefix) + "）", "gap"
	case expr == "project":
		return c.p.Config.Project, ""
	case expr == "profile":
		return or(c.p.Schema.ProfileLabel, c.p.Schema.Profile), ""
	case expr == "today":
		return time.Now().Format("2006/01/02"), ""
	case expr == "config.scope":
		if len(c.p.Config.Scope) == 0 {
			return "（対象範囲が .canon/config.yaml の scope に書かれていません）", "gap"
		}
		return strings.Join(c.p.Config.Scope, "\n"), ""
	case strings.HasPrefix(expr, "stat."):
		s := gaps.Build(c.p, false).Summary
		switch strings.TrimPrefix(expr, "stat.") {
		case "confirmed_rate":
			return fmt.Sprintf("%.0f%%（確定 %d／全 %d 項目）", s.ConfirmedRate*100, s.Confirmed+s.NotApplicable, s.Fields), ""
		case "open_questions":
			return fmt.Sprintf("%d 件（うち着手条件 %d 件）", s.OpenQuestions, s.BlockingGaps), ""
		}
	}
	if e == nil {
		return "（不明な式: " + expr + "）", "gap"
	}
	switch expr {
	case "id":
		return e.ID, ""
	case "name":
		return e.Name, ""
	case "type":
		return e.Type, ""
	}
	parts := strings.Split(expr, ".")
	switch parts[0] {
	case "fields":
		if len(parts) < 2 {
			return "", ""
		}
		if td := c.p.Schema.Types[e.Type]; td != nil && td.Fields[parts[1]] == nil {
			return "（Canon の" + or(td.Label, e.Type) + "に項目「" + parts[1] + "」がありません）", "gap"
		}
		fl := e.Fields[parts[1]]
		if len(parts) == 3 {
			switch parts[2] {
			case "source":
				return sourceText(fl), ""
			case "status":
				return StatusLabel(fl), statusOf(fl)
			}
		}
		if fl == nil {
			return "（記載なし）", model.StatusUnknown
		}
		return FieldText(fl), statusOf(fl)
	case "links", "inbound":
		if len(parts) == 2 {
			var v string
			if parts[0] == "links" {
				v = linked(c.p, c.g.Out[e.ID], parts[1], false)
			} else {
				v = linked(c.p, c.g.In[e.ID], parts[1], true)
			}
			if v == "" {
				return "（Canon に対応付けがありません）", "gap"
			}
			return v, ""
		}
	}
	return "（不明な式: " + expr + "）", "gap"
}

// exprState は式が Canon のどこにも置き場が無いかを判定する。
func (c *tctx) exprState(typ, expr string) string {
	expr = strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(expr, "="):
		return "blank"
	case strings.HasPrefix(expr, gapPrefix):
		return "no_place"
	case expr == "config.scope" && len(c.p.Config.Scope) == 0:
		return "no_elements"
	}
	if typ != "" {
		td := c.p.Schema.Types[typ]
		if td == nil {
			return "no_type"
		}
		if strings.HasPrefix(expr, "fields.") {
			fn := strings.Split(strings.TrimPrefix(expr, "fields."), ".")[0]
			if td.Fields[fn] == nil {
				return "no_field"
			}
		}
	}
	return "ok"
}

func (c *tctx) typeState(typ string) string {
	if c.p.Schema.Types[typ] == nil {
		return "no_type"
	}
	return "no_elements"
}

func (c *tctx) typeMessage(typ string) string {
	if c.p.Schema.Types[typ] == nil {
		return "（Canon に「" + typ + "」の型がありません。この表を埋める情報の置き場が無い状態です）"
	}
	return "（該当なし：Canon に" + or(c.p.Schema.Types[typ].Label, typ) + "の要素がありません）"
}

func (c *tctx) record(sheet string, x Expr, kind string, confirmed, total int) {
	key := sheet + "\x00" + x.Expr + "\x00" + x.Label
	cv, ok := c.cov[key]
	if !ok {
		cv = &Coverage{Sheet: sheet, Label: or(x.Label, x.Expr), Expr: x.Expr, Kind: kind}
		c.cov[key] = cv
		c.order = append(c.order, key)
	}
	cv.Confirmed += confirmed
	cv.Total += total
	if cv.Kind == "ok" && cv.Total > 0 && cv.Confirmed < cv.Total {
		cv.Kind = "partial"
	}
	if kind != "ok" && kind != "partial" {
		cv.Kind = kind
	}
}

func (c *tctx) writeNote(sheet, addr, msg string) {
	style, _ := c.f.GetCellStyle(sheet, addr)
	c.write(sheet, addr, style, msg, "gap")
}

func (c *tctx) write(sheet, addr string, base int, v, status string) {
	_ = c.f.SetCellStr(sheet, addr, v)
	color := map[string]string{model.StatusUnknown: colorUnknown, model.StatusConflict: colorConflict, model.StatusNA: colorNA, "gap": "F2F2F2"}[status]
	if color == "" {
		if base != 0 {
			_ = c.f.SetCellStyle(sheet, addr, addr, base)
		}
		return
	}
	key := fmt.Sprintf("%d/%s", base, status)
	id, ok := c.filled[key]
	if !ok {
		st := &excelize.Style{}
		if base != 0 {
			if s, err := c.f.GetStyle(base); err == nil && s != nil {
				st = s
				fixColors(st)
			}
		}
		st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{color}}
		if status == "gap" {
			if st.Font == nil {
				st.Font = &excelize.Font{}
			}
			st.Font.Color = "808080"
			st.Font.Italic = true
		}
		if st.Alignment == nil {
			st.Alignment = &excelize.Alignment{WrapText: true, Vertical: "top"}
		} else {
			st.Alignment.WrapText = true
		}
		id, _ = c.f.NewStyle(st)
		c.filled[key] = id
	}
	_ = c.f.SetCellStyle(sheet, addr, addr, id)
}

func statusOf(f *model.Field) string {
	if f == nil {
		return model.StatusUnknown
	}
	if f.Status == model.StatusConfirmed {
		return ""
	}
	return f.Status
}

func sortedQuestions(p *project.Project) []*model.Question {
	var qs []*model.Question
	for _, q := range p.Questions {
		qs = append(qs, q)
	}
	rank := map[string]int{"open": 0, "answered": 1, "withdrawn": 2}
	sort.SliceStable(qs, func(i, j int) bool {
		a, b := qs[i], qs[j]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		if a.Blocking != b.Blocking {
			return a.Blocking
		}
		return a.ID < b.ID
	})
	return qs
}

func questionValue(p *project.Project, q *model.Question, expr string) string {
	if strings.HasPrefix(expr, "=") {
		return strings.TrimPrefix(expr, "=")
	}
	row := QuestionRow(p, q)
	idx := map[string]int{"id": 0, "blocking": 1, "to": 2, "asks_for": 3, "context": 4, "options": 5, "status": 6, "answer.value": 7, "answer.by": 8, "answer.at": 9}
	if i, ok := idx[strings.TrimSpace(expr)]; ok {
		return row[i]
	}
	return "（不明な式: " + expr + "）"
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		ci, ri, _ := excelize.SplitCellName(ks[i])
		cj, rj, _ := excelize.SplitCellName(ks[j])
		if ri != rj {
			return ri < rj
		}
		if len(ci) != len(cj) {
			return len(ci) < len(cj)
		}
		return ci < cj
	})
	return ks
}

// fixColors は読み込んだスタイルの色を NewStyle が受け付ける6桁の RGB に揃える
// （テンプレートの aRGB 8桁をそのまま渡すと不正な10桁の色になる）。
func fixColors(st *excelize.Style) {
	six := func(c string) string {
		c = strings.TrimPrefix(c, "#")
		if len(c) > 6 {
			c = c[len(c)-6:]
		}
		return c
	}
	if st.Font != nil {
		st.Font.Color = six(st.Font.Color)
	}
	for i := range st.Border {
		st.Border[i].Color = six(st.Border[i].Color)
	}
	for i := range st.Fill.Color {
		st.Fill.Color[i] = six(st.Fill.Color[i])
	}
}

// CoverageMarkdown は不足一覧を Markdown にする。
func CoverageMarkdown(doc string, cov []Coverage) string {
	label := map[string]string{
		"ok": "充足", "partial": "一部未確定", "no_type": "型が無い", "no_field": "項目が無い",
		"no_place": "置き場が無い", "no_elements": "要素が0件", "blank": "（人が記入）",
	}
	count := map[string]int{}
	for _, c := range cov {
		count[c.Kind]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# 不足一覧：%s\n\n", doc)
	fmt.Fprintf(&b, "| 区分 | 項目数 |\n|---|---|\n")
	for _, k := range []string{"ok", "partial", "no_elements", "no_field", "no_type", "no_place", "blank"} {
		if count[k] > 0 {
			fmt.Fprintf(&b, "| %s | %d |\n", label[k], count[k])
		}
	}
	b.WriteString("\n| シート | 項目 | 区分 | 確定 | 式 |\n|---|---|---|---|---|\n")
	for _, c := range cov {
		if c.Kind == "blank" {
			continue
		}
		ratio := "-"
		if c.Total > 0 {
			ratio = fmt.Sprintf("%d/%d", c.Confirmed, c.Total)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | `%s` |\n", c.Sheet, c.Label, label[c.Kind], ratio, c.Expr)
	}
	return b.String()
}
