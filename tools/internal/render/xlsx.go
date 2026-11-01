package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/gaps"
	"github.com/bubu-xx/canon/tools/internal/graph"
	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
	"github.com/bubu-xx/canon/tools/internal/schema"
	"github.com/xuri/excelize/v2"
)

const font = "Meiryo UI"

// 状態ごとのセルの色。
const (
	colorHeader   = "1F4E79"
	colorLabel    = "DDEBF7"
	colorUnknown  = "FFF2CC"
	colorConflict = "F8CBAD"
	colorNA       = "EDEDED"
)

type styles struct {
	f                                                      *excelize.File
	title, sub, header, body, label, unknown, conflict, na int
}

func newStyles(f *excelize.File) (*styles, error) {
	s := &styles{f: f}
	border := []excelize.Border{
		{Type: "left", Color: "8EA9C1", Style: 1}, {Type: "right", Color: "8EA9C1", Style: 1},
		{Type: "top", Color: "8EA9C1", Style: 1}, {Type: "bottom", Color: "8EA9C1", Style: 1},
	}
	wrap := &excelize.Alignment{WrapText: true, Vertical: "top"}
	mk := func(st *excelize.Style) int {
		id, err := f.NewStyle(st)
		if err != nil {
			panic(err)
		}
		return id
	}
	fill := func(c string) excelize.Fill { return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{c}} }
	s.title = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 16, Bold: true, Color: colorHeader}})
	s.sub = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 9, Color: "595959"}, Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	s.header = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Bold: true, Color: "FFFFFF"}, Fill: fill(colorHeader), Border: border,
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center", Horizontal: "center"}})
	s.body = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10}, Border: border, Alignment: wrap})
	s.label = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Bold: true}, Fill: fill(colorLabel), Border: border, Alignment: wrap})
	s.unknown = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Bold: true, Color: "7F6000"}, Fill: fill(colorUnknown), Border: border, Alignment: wrap})
	s.conflict = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Bold: true, Color: "843C0C"}, Fill: fill(colorConflict), Border: border, Alignment: wrap})
	s.na = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Color: "7F7F7F"}, Fill: fill(colorNA), Border: border, Alignment: wrap})
	return s, nil
}

func (s *styles) forStatus(status string) int {
	switch status {
	case model.StatusUnknown:
		return s.unknown
	case model.StatusConflict:
		return s.conflict
	case model.StatusNA:
		return s.na
	}
	return s.body
}

// FieldText は項目をセルに入れる文字列にする。未確定・矛盾は質問 ID とともに明示する。
func FieldText(f *model.Field) string {
	if f == nil {
		return "（記載なし）"
	}
	switch f.Status {
	case model.StatusConfirmed:
		s := cellValue(f.Value)
		if f.Unit != "" {
			s += " " + f.Unit
		}
		return s
	case model.StatusUnknown:
		return fmt.Sprintf("未確定（%s）", f.Question)
	case model.StatusConflict:
		lines := []string{fmt.Sprintf("矛盾あり・未確定（%s）", f.Question)}
		for _, c := range f.Claims {
			lines = append(lines, fmt.Sprintf("・%s：%s", c.Source.String(), inline(c.Value)))
		}
		return strings.Join(lines, "\n")
	case model.StatusNA:
		return "該当なし"
	}
	return ""
}

// StatusLabel は状態の日本語名。
func StatusLabel(f *model.Field) string {
	if f == nil {
		return "記載なし"
	}
	switch f.Status {
	case model.StatusConfirmed:
		return "確定"
	case model.StatusUnknown:
		return "未確定"
	case model.StatusConflict:
		return "矛盾"
	case model.StatusNA:
		return "該当なし"
	}
	return f.Status
}

// enumLabels は列挙値の日本語表記。
var enumLabels = map[string]string{
	"functional": "機能要求", "non_functional": "非機能要求", "constraint": "制約",
	"must": "必須（must）", "should": "推奨（should）", "could": "任意（could）",
	"unit": "単体", "integration": "結合", "system": "システム", "acceptance": "受入",
	"idempotent": "冪等", "not_idempotent": "冪等でない",
	"availability": "可用性", "performance": "性能・拡張性", "operability": "運用・保守性", "migration": "移行性",
	"security": "セキュリティ", "environment": "システム環境・エコロジー",
	"normal": "正常系", "abnormal": "異常系", "boundary": "境界値",
	"function": "機能", "reliability": "信頼性", "usability": "使用性", "compatibility": "互換性", "operational": "運用",
	"inbound": "受信", "outbound": "送信", "bidirectional": "双方向",
}

func lines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func cellValue(v any) string {
	if s, ok := v.(string); ok {
		if l, ok := enumLabels[s]; ok {
			return l
		}
	}
	switch t := v.(type) {
	case []any:
		if len(t) == 0 {
			return "（なし）"
		}
		lines := make([]string, len(t))
		for i, x := range t {
			lines[i] = "・" + inline(x)
		}
		return strings.Join(lines, "\n")
	default:
		return inline(v)
	}
}

func sourceText(f *model.Field) string {
	if f == nil {
		return ""
	}
	if f.Status == model.StatusConfirmed || f.Status == model.StatusNA {
		return f.Source.String()
	}
	return ""
}

// WriteXLSX は標準様式の Excel 設計書を書く。
func WriteXLSX(p *project.Project, out string) error {
	f := excelize.NewFile()
	defer f.Close()
	st, err := newStyles(f)
	if err != nil {
		return err
	}
	g := graph.Build(p)
	report := gaps.Build(p, false)

	cover := "表紙"
	_ = f.SetSheetName("Sheet1", cover)
	var sheets [][2]string

	for _, t := range p.Schema.TypeOrder {
		td := p.Schema.Types[t]
		if t == "bhv" {
			for _, id := range p.Order {
				e := p.Elements[id]
				if e.Type != "bhv" {
					continue
				}
				name := sheetName(e.ID + " " + e.Name)
				if err := elementSheet(f, st, p, g, e, td, name); err != nil {
					return err
				}
				sheets = append(sheets, [2]string{name, "処理仕様（振る舞い1件につき1シート）"})
			}
			continue
		}
		name := map[string]string{"req": "要件一覧", "tst": "試験仕様書"}[t]
		if name == "" {
			name = sheetName(or(td.Label, t) + "一覧")
		}
		if err := tableSheet(f, st, p, g, td, name); err != nil {
			return err
		}
		sheets = append(sheets, [2]string{name, or(td.Label, t) + "の一覧"})
	}
	if err := traceSheet(f, st, p, g); err != nil {
		return err
	}
	sheets = append(sheets, [2]string{"トレーサビリティ", "要求・振る舞い・試験の対応"})
	if err := questionSheet(f, st, p); err != nil {
		return err
	}
	sheets = append(sheets, [2]string{"課題管理表", "未確定事項（質問）と回答"})
	coverSheet(f, st, p, report, cover, sheets)
	for _, sh := range f.GetSheetList() {
		autofit(f, sh)
	}
	f.SetActiveSheet(0)
	return f.SaveAs(out)
}

func coverSheet(f *excelize.File, st *styles, p *project.Project, r *gaps.Report, name string, sheets [][2]string) {
	_ = f.SetColWidth(name, "A", "A", 22)
	_ = f.SetColWidth(name, "B", "B", 70)
	set(f, name, "A1", p.Config.Project+" 設計書", st.title)
	set(f, name, "A2", "この文書は Canon（複数の情報源を照合して確定させた、検証済みの正解情報）から canon render で機械的に生成しています。手で編集せず、変更は Canon に対して行ってください。", st.sub)
	_ = f.MergeCell(name, "A2", "B2")
	_ = f.SetRowHeight(name, 2, 30)
	s := r.Summary
	var els []string
	for _, t := range p.Schema.TypeOrder {
		if n := s.Elements[t]; n > 0 {
			els = append(els, fmt.Sprintf("%s %d", or(p.Schema.Types[t].Label, t), n))
		}
	}
	rows := [][2]string{
		{"プロジェクトの種類", or(p.Schema.ProfileLabel, p.Schema.Profile)},
		{"要素", strings.Join(els, "、")},
		{"項目", fmt.Sprintf("%d（確定 %d・該当なし %d・未確定 %d・矛盾 %d）", s.Fields, s.Confirmed, s.NotApplicable, s.Unknown, s.Conflict)},
		{"確定率", fmt.Sprintf("%.0f%%", s.ConfirmedRate*100)},
		{"着手条件が欠けている要素", fmt.Sprintf("%d（必須の未確定・矛盾 %d 件）", s.BlockedElements, s.BlockingGaps)},
		{"未回答の質問", fmt.Sprintf("%d", s.OpenQuestions)},
	}
	row := 4
	for _, kv := range rows {
		set(f, name, cell("A", row), kv[0], st.label)
		set(f, name, cell("B", row), kv[1], st.body)
		row++
	}
	row++
	set(f, name, cell("A", row), "シート", st.header)
	set(f, name, cell("B", row), "内容", st.header)
	row++
	for _, sh := range sheets {
		set(f, name, cell("A", row), sh[0], st.body)
		_ = f.SetCellHyperLink(name, cell("A", row), "'"+sh[0]+"'!A1", "Location")
		set(f, name, cell("B", row), sh[1], st.body)
		row++
	}
	row++
	set(f, name, cell("A", row), "凡例", st.header)
	set(f, name, cell("B", row), "", st.header)
	legend := []struct {
		label string
		style int
		desc  string
	}{
		{"確定", st.body, "出典（情報源の位置・人の回答）が付いた値。出典はセルのコメントに表示"},
		{"未確定（Q-xxx）", st.unknown, "どの情報源にも根拠が無い。課題管理表の質問に回答が必要"},
		{"矛盾あり・未確定（Q-xxx）", st.conflict, "情報源どうしが食い違っている。どちらが正しいか回答が必要"},
		{"該当なし", st.na, "この要素には当てはまらない（根拠付きの判断）"},
	}
	for _, l := range legend {
		row++
		set(f, name, cell("A", row), l.label, l.style)
		set(f, name, cell("B", row), l.desc, st.body)
	}
	setPrint(f, name, false)
}

func elementSheet(f *excelize.File, st *styles, p *project.Project, g *graph.Graph, e *model.Element, td *schema.TypeDef, name string) error {
	if _, err := f.NewSheet(name); err != nil {
		return err
	}
	_ = f.SetColWidth(name, "A", "A", 18)
	_ = f.SetColWidth(name, "B", "B", 72)
	_ = f.SetColWidth(name, "C", "C", 10)
	_ = f.SetColWidth(name, "D", "D", 34)
	set(f, name, "A1", "処理仕様", st.title)
	head := [][2]string{
		{"ID", e.ID},
		{"名称", e.Name},
		{"派生元の要求", linked(p, g.Out[e.ID], "derived_from", false)},
		{"検証する試験", strings.Join(uniq(append(lines(linked(p, g.Out[e.ID], "verified_by", false)), lines(linked(p, g.In[e.ID], "verifies", true))...)), "\n")},
	}
	row := 3
	for _, kv := range head {
		set(f, name, cell("A", row), kv[0], st.label)
		set(f, name, cell("B", row), or(strings.TrimSpace(kv[1]), "（なし）"), st.body)
		_ = f.MergeCell(name, cell("B", row), cell("D", row))
		row++
	}
	row++
	for i, h := range []string{"項目", "内容", "状態", "出典"} {
		set(f, name, cell(string(rune('A'+i)), row), h, st.header)
	}
	_ = f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: row, TopLeftCell: cell("A", row+1), ActivePane: "bottomLeft"})
	row++
	for _, fn := range td.FieldOrder {
		def := td.Fields[fn]
		fl := e.Fields[fn]
		if fl == nil && !def.Required {
			continue
		}
		label := def.Label
		if def.Blocking {
			label += " ※"
		}
		status := ""
		if fl != nil {
			status = fl.Status
		}
		set(f, name, cell("A", row), label, st.label)
		set(f, name, cell("B", row), FieldText(fl), st.forStatus(status))
		set(f, name, cell("C", row), StatusLabel(fl), st.forStatus(status))
		set(f, name, cell("D", row), sourceText(fl), st.body)
		row++
	}
	row++
	set(f, name, cell("A", row), "※ は着手条件（未確定・矛盾のままだと、この振る舞いの実装・修正に着手しない項目）", st.sub)
	_ = f.MergeCell(name, cell("A", row), cell("D", row))
	setPrint(f, name, false)
	return nil
}

func tableSheet(f *excelize.File, st *styles, p *project.Project, g *graph.Graph, td *schema.TypeDef, name string) error {
	if _, err := f.NewSheet(name); err != nil {
		return err
	}
	var cols []string
	for _, fn := range td.FieldOrder {
		if td.Fields[fn].Required || anyHas(p, td.Name, fn) {
			cols = append(cols, fn)
		}
	}
	extra := map[string]string{"req": "実現する振る舞い", "tst": "検証対象"}[td.Name]
	headers := []string{"ID", "名称"}
	for _, fn := range cols {
		h := td.Fields[fn].Label
		if td.Fields[fn].Blocking {
			h += " ※"
		}
		headers = append(headers, h)
	}
	if extra != "" {
		headers = append(headers, extra)
	}
	set(f, name, "A1", or(td.Label, td.Name)+"一覧", st.title)
	hr := 3
	for i, h := range headers {
		c := colName(i + 1)
		set(f, name, cell(c, hr), h, st.header)
		w := 30.0
		switch {
		case i == 0:
			w = 10
		case i == 1:
			w = 24
		}
		_ = f.SetColWidth(name, c, c, w)
	}
	row := hr + 1
	for _, id := range p.Order {
		e := p.Elements[id]
		if e.Type != td.Name {
			continue
		}
		set(f, name, cell("A", row), e.ID, st.body)
		set(f, name, cell("B", row), e.Name, st.body)
		for i, fn := range cols {
			fl := e.Fields[fn]
			c := cell(colName(i+3), row)
			status := ""
			if fl != nil {
				status = fl.Status
			}
			set(f, name, c, FieldText(fl), st.forStatus(status))
			if src := sourceText(fl); src != "" {
				comment(f, name, c, "出典: "+src)
			}
		}
		if extra != "" {
			var v string
			if td.Name == "req" {
				v = linked(p, g.In[e.ID], "derived_from", true)
			} else {
				v = linked(p, g.Out[e.ID], "verifies", false)
			}
			set(f, name, cell(colName(len(cols)+3), row), or(v, "（なし）"), st.body)
		}
		row++
	}
	if row == hr+1 {
		set(f, name, cell("A", row), "（該当なし）", st.body)
	}
	last := colName(len(headers))
	_ = f.AutoFilter(name, fmt.Sprintf("A%d:%s%d", hr, last, max(row-1, hr)), nil)
	_ = f.SetPanes(name, &excelize.Panes{Freeze: true, XSplit: 2, YSplit: hr, TopLeftCell: cell("C", hr+1), ActivePane: "bottomRight"})
	set(f, name, cell("A", row+1), "※ は着手条件。出典は各セルのコメントに表示", st.sub)
	setPrint(f, name, true)
	return nil
}

func traceSheet(f *excelize.File, st *styles, p *project.Project, g *graph.Graph) error {
	name := "トレーサビリティ"
	if _, err := f.NewSheet(name); err != nil {
		return err
	}
	set(f, name, "A1", "トレーサビリティ", st.title)
	heads := []string{"要求", "名称", "振る舞い", "試験", "判定"}
	widths := []float64{10, 30, 34, 34, 22}
	for i, h := range heads {
		c := colName(i + 1)
		set(f, name, cell(c, 3), h, st.header)
		_ = f.SetColWidth(name, c, c, widths[i])
	}
	row := 4
	for _, id := range p.Order {
		e := p.Elements[id]
		if e.Type != "req" {
			continue
		}
		var bhvs, tsts []string
		for _, ed := range g.In[e.ID] {
			from := p.Elements[ed.From]
			if from == nil {
				continue
			}
			if ed.Type == "derived_from" && from.Type == "bhv" {
				bhvs = append(bhvs, from.ID+" "+from.Name)
				for _, x := range g.Out[from.ID] {
					if x.Type == "verified_by" && p.Elements[x.To] != nil {
						tsts = append(tsts, x.To+" "+p.Elements[x.To].Name)
					}
				}
				for _, x := range g.In[from.ID] {
					if x.Type == "verifies" {
						tsts = append(tsts, x.From+" "+p.Elements[x.From].Name)
					}
				}
			}
			if ed.Type == "verifies" {
				tsts = append(tsts, from.ID+" "+from.Name)
			}
		}
		bhvs, tsts = uniq(bhvs), uniq(tsts)
		judge, style := "OK", st.body
		switch {
		case len(bhvs) == 0:
			judge, style = "振る舞いが無い（未実現の可能性）", st.unknown
		case len(tsts) == 0:
			judge, style = "試験が無い（未検証）", st.unknown
		}
		set(f, name, cell("A", row), e.ID, st.body)
		set(f, name, cell("B", row), e.Name, st.body)
		set(f, name, cell("C", row), or(strings.Join(bhvs, "\n"), "（なし）"), st.body)
		set(f, name, cell("D", row), or(strings.Join(tsts, "\n"), "（なし）"), st.body)
		set(f, name, cell("E", row), judge, style)
		row++
	}
	if row == 4 {
		set(f, name, "A4", "（要求なし）", st.body)
	}
	setPrint(f, name, true)
	return nil
}

func questionSheet(f *excelize.File, st *styles, p *project.Project) error {
	name := "課題管理表"
	if _, err := f.NewSheet(name); err != nil {
		return err
	}
	set(f, name, "A1", "課題管理表（未確定事項）", st.title)
	heads := []string{"ID", "区分", "宛先", "対象項目", "内容", "回答候補", "状態", "回答", "回答者", "回答日"}
	widths := []float64{8, 8, 12, 26, 50, 36, 9, 30, 12, 14}
	for i, h := range heads {
		c := colName(i + 1)
		set(f, name, cell(c, 3), h, st.header)
		_ = f.SetColWidth(name, c, c, widths[i])
	}
	ids := make([]string, 0, len(p.Questions))
	for id := range p.Questions {
		ids = append(ids, id)
	}
	rank := map[string]int{"open": 0, "answered": 1, "withdrawn": 2}
	sort.Slice(ids, func(i, j int) bool {
		a, b := p.Questions[ids[i]], p.Questions[ids[j]]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		if a.Blocking != b.Blocking {
			return a.Blocking
		}
		return a.ID < b.ID
	})
	row := 4
	for _, id := range ids {
		q := p.Questions[id]
		vals := QuestionRow(p, q)
		style := st.body
		if q.Status == "open" {
			style = st.unknown
			if q.Blocking {
				style = st.conflict
			}
		}
		for i, v := range vals {
			s := st.body
			if i == 1 || i == 6 {
				s = style
			}
			set(f, name, cell(colName(i+1), row), v, s)
		}
		row++
	}
	if row == 4 {
		set(f, name, "A4", "（質問なし）", st.body)
	}
	_ = f.AutoFilter(name, fmt.Sprintf("A3:J%d", max(row-1, 3)), nil)
	_ = f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 3, TopLeftCell: "A4", ActivePane: "bottomLeft"})
	set(f, name, cell("A", row+1), "回答は人が記入し、/canon:ask で Canon に取り込む。AI は回答を作らない。", st.sub)
	setPrint(f, name, true)
	return nil
}

// QuestionRow は課題管理表の1行。
func QuestionRow(p *project.Project, q *model.Question) []string {
	kind := "注意"
	if q.Blocking {
		kind = "必須"
	}
	var targets []string
	for _, ref := range q.AsksFor {
		id, fn, _ := strings.Cut(ref, ".")
		lbl := fn
		if e := p.Elements[id]; e != nil {
			if td := p.Schema.Types[e.Type]; td != nil && td.Fields[fn] != nil {
				lbl = td.Fields[fn].Label
			}
		}
		targets = append(targets, fmt.Sprintf("%s %s", id, lbl))
	}
	var opts []string
	for _, o := range q.Options {
		opts = append(opts, "・"+inline(o))
	}
	status := map[string]string{"open": "未回答", "answered": "回答済", "withdrawn": "取下げ"}[q.Status]
	ans := func(k string) string {
		if q.Answer == nil {
			return ""
		}
		return inline(q.Answer[k])
	}
	return []string{q.ID, kind, q.To, strings.Join(targets, "\n"), q.Context, strings.Join(opts, "\n"), or(status, q.Status), ans("value"), ans("by"), ans("at")}
}

func linked(p *project.Project, edges []graph.Edge, typ string, from bool) string {
	var out []string
	for _, ed := range edges {
		if ed.Type != typ {
			continue
		}
		id := ed.To
		if from {
			id = ed.From
		}
		if e := p.Elements[id]; e != nil {
			out = append(out, id+" "+e.Name)
		} else {
			out = append(out, id)
		}
	}
	return strings.Join(uniq(out), "\n")
}

func anyHas(p *project.Project, typ, field string) bool {
	for _, e := range p.Elements {
		if e.Type == typ {
			if _, ok := e.Fields[field]; ok {
				return true
			}
		}
	}
	return false
}

func set(f *excelize.File, sheet, c, v string, style int) {
	_ = f.SetCellStr(sheet, c, v)
	_ = f.SetCellStyle(sheet, c, c, style)
}

func comment(f *excelize.File, sheet, c, text string) {
	_ = f.AddComment(sheet, excelize.Comment{Cell: c, Author: "canon", Paragraph: []excelize.RichTextRun{{Text: text}}})
}

func setPrint(f *excelize.File, sheet string, landscape bool) {
	o := "portrait"
	if landscape {
		o = "landscape"
	}
	fit := true
	one := 1
	zero := 0
	_ = f.SetPageLayout(sheet, &excelize.PageLayoutOptions{Orientation: &o, FitToWidth: &one, FitToHeight: &zero})
	_ = f.SetSheetProps(sheet, &excelize.SheetPropsOptions{FitToPage: &fit})
}

func cell(col string, row int) string { return fmt.Sprintf("%s%d", col, row) }

func colName(n int) string {
	s, _ := excelize.ColumnNumberToName(n)
	return s
}

// sheetName は Excel のシート名に使える形にする。Excel は : \\ / ? * [ ] を全角でも拒否するため、
// 半角・全角とも「_」に置き換え、連続を1つにまとめ、31文字に切り詰める。
func sheetName(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if strings.ContainsRune(":\\/?*[]：＼／？＊［］", r) {
			if !prev {
				b.WriteRune('_')
			}
			prev = true
			continue
		}
		prev = false
		b.WriteRune(r)
	}
	rs := []rune(strings.Trim(b.String(), "'"))
	if len(rs) > 31 {
		rs = rs[:31]
	}
	return strings.TrimSpace(strings.Trim(string(rs), "'"))
}
