// Package render は Canon から人向け文書を決定的に生成する（LLM は使わない）。
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/graph"
	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
)

// Write は outDir に文書を書き出し、書いたファイルの一覧を返す。
func Write(p *project.Project, outDir string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	docs := map[string]string{}
	names := map[string]string{"req": "要件一覧.md", "bhv": "振る舞い仕様.md", "tst": "試験仕様.md"}
	for _, t := range p.Schema.TypeOrder {
		td := p.Schema.Types[t]
		name, ok := names[t]
		if !ok {
			name = td.Label + ".md"
			if td.Label == "" {
				name = t + ".md"
			}
		}
		docs[name] = typeDoc(p, t)
	}
	docs["未確定事項.md"] = openItems(p)
	docs["トレーサビリティ.md"] = trace(p)
	var written []string
	keys := make([]string, 0, len(docs))
	for k := range docs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		path := filepath.Join(outDir, k)
		if err := os.WriteFile(path, []byte(docs[k]), 0o644); err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}

const header = "<!-- canon render で Canon から生成。手で編集しないこと（変更は Canon に対して行う） -->\n\n"

func typeDoc(p *project.Project, t string) string {
	td := p.Schema.Types[t]
	var b strings.Builder
	b.WriteString(header)
	fmt.Fprintf(&b, "# %s\n\n", or(td.Label, t))
	n := 0
	for _, id := range p.Order {
		e := p.Elements[id]
		if e.Type != t {
			continue
		}
		n++
		fmt.Fprintf(&b, "## %s %s\n\n", e.ID, e.Name)
		for _, fname := range td.FieldOrder {
			f, ok := e.Fields[fname]
			def := td.Fields[fname]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "### %s\n\n", def.Label)
			switch f.Status {
			case model.StatusConfirmed:
				b.WriteString(value(f.Value, f.Unit))
				fmt.Fprintf(&b, "\n<sub>出典: %s</sub>\n\n", f.Source.String())
			case model.StatusNA:
				fmt.Fprintf(&b, "該当なし\n\n<sub>根拠: %s</sub>\n\n", f.Source.String())
			case model.StatusUnknown:
				fmt.Fprintf(&b, "**未確定（%s）**\n\n", f.Question)
			case model.StatusConflict:
				fmt.Fprintf(&b, "**矛盾あり・未確定（%s）**\n\n", f.Question)
				for _, cl := range f.Claims {
					fmt.Fprintf(&b, "- %s の記述: %s\n", cl.Source.String(), inline(cl.Value))
				}
				b.WriteString("\n")
			}
		}
		if len(e.Links) > 0 {
			b.WriteString("### 関連\n\n")
			for _, l := range e.Links {
				fmt.Fprintf(&b, "- %s → %s %s\n", linkLabel(l.Type), l.Target, nameOf(p, l.Target))
			}
			b.WriteString("\n")
		}
	}
	if n == 0 {
		b.WriteString("（該当なし）\n")
	}
	return b.String()
}

func openItems(p *project.Project) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("# 未確定事項\n\n")
	var ids []string
	for id, q := range p.Questions {
		if q.Status == "open" {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, c := p.Questions[ids[i]], p.Questions[ids[j]]
		if a.Blocking != c.Blocking {
			return a.Blocking
		}
		return a.ID < c.ID
	})
	if len(ids) == 0 {
		b.WriteString("未回答の質問はありません。\n")
		return b.String()
	}
	b.WriteString("| ID | 区分 | 宛先 | 対象 | 内容 |\n|---|---|---|---|---|\n")
	for _, id := range ids {
		q := p.Questions[id]
		mark := "注意"
		if q.Blocking {
			mark = "必須"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", q.ID, mark, or(q.To, "-"), strings.Join(q.AsksFor, "<br>"), mdCell(q.Context))
	}
	for _, id := range ids {
		q := p.Questions[id]
		if len(q.Options) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n**%s の回答候補**（既定値ではない）\n\n", q.ID)
		for _, o := range q.Options {
			fmt.Fprintf(&b, "- %s\n", inline(o))
		}
	}
	return b.String()
}

func trace(p *project.Project) string {
	g := graph.Build(p)
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("# トレーサビリティ\n\n| 要求 | 振る舞い | 試験 |\n|---|---|---|\n")
	any := false
	for _, id := range p.Order {
		e := p.Elements[id]
		if e.Type != "req" {
			continue
		}
		any = true
		var bhvs, tsts []string
		for _, ed := range g.In[e.ID] {
			from := p.Elements[ed.From]
			if from == nil {
				continue
			}
			switch {
			case ed.Type == "derived_from" && from.Type == "bhv":
				bhvs = append(bhvs, from.ID)
				for _, x := range g.Out[from.ID] {
					if x.Type == "verified_by" {
						tsts = append(tsts, x.To)
					}
				}
				for _, x := range g.In[from.ID] {
					if x.Type == "verifies" {
						tsts = append(tsts, x.From)
					}
				}
			case ed.Type == "verifies":
				tsts = append(tsts, from.ID)
			}
		}
		fmt.Fprintf(&b, "| %s %s | %s | %s |\n", e.ID, e.Name, list(uniq(bhvs)), list(uniq(tsts)))
	}
	if !any {
		b.WriteString("| （要求なし） | | |\n")
	}
	return b.String()
}

func value(v any, unit string) string {
	switch t := v.(type) {
	case []any:
		if len(t) == 0 {
			return "（なし）\n"
		}
		var b strings.Builder
		for _, x := range t {
			fmt.Fprintf(&b, "- %s\n", inline(x))
		}
		return b.String()
	default:
		s := inline(v)
		if unit != "" {
			s += " " + unit
		}
		return s + "\n"
	}
}

func inline(v any) string {
	switch t := v.(type) {
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = inline(x)
		}
		return strings.Join(parts, " / ")
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func linkLabel(t string) string {
	m := map[string]string{"derived_from": "派生元", "realized_by": "実現", "verified_by": "検証する試験", "verifies": "検証対象", "uses": "依存", "decided_by": "設計判断", "defines": "用語"}
	if v, ok := m[t]; ok {
		return v
	}
	return t
}

func nameOf(p *project.Project, id string) string {
	if e, ok := p.Elements[id]; ok {
		return e.Name
	}
	return "（存在しない）"
}

func mdCell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "｜"), "\n", " ") }

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func list(xs []string) string {
	if len(xs) == 0 {
		return "**なし**"
	}
	return strings.Join(xs, ", ")
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
