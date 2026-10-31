// Package gaps は空白・矛盾・リンク不足を一覧にする。
package gaps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/graph"
	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
)

// FieldGap は unknown / conflict の項目。
type FieldGap struct {
	Element  string `json:"element"`
	Type     string `json:"type"`
	Field    string `json:"field"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Blocking bool   `json:"blocking"`
	Question string `json:"question,omitempty"`
}

// LinkGap は必須リンクの不足。
type LinkGap struct {
	Element  string `json:"element"`
	Kind     string `json:"kind"` // missing_link / missing_inbound
	LinkType string `json:"link_type"`
	Need     int    `json:"need"`
	Have     int    `json:"have"`
	Message  string `json:"message"`
}

// QuestionSummary は質問の要約。
type QuestionSummary struct {
	ID       string   `json:"id"`
	Blocking bool     `json:"blocking"`
	To       string   `json:"to,omitempty"`
	AsksFor  []string `json:"asks_for"`
	Status   string   `json:"status"`
	Context  string   `json:"context,omitempty"`
}

// Summary は全体の数字。
type Summary struct {
	Elements        map[string]int `json:"elements"`
	Fields          int            `json:"fields"`
	Confirmed       int            `json:"confirmed"`
	Unknown         int            `json:"unknown"`
	Conflict        int            `json:"conflict"`
	NotApplicable   int            `json:"not_applicable"`
	ConfirmedRate   float64        `json:"confirmed_rate"`
	BlockingGaps    int            `json:"blocking_gaps"`
	ReadyElements   int            `json:"ready_elements"`
	BlockedElements int            `json:"blocked_elements"`
	LinkGaps        int            `json:"link_gaps"`
	OpenQuestions   int            `json:"open_questions"`
	SourceAnchors   int            `json:"source_anchors"`
	SourceUsed      int            `json:"source_used"`
}

// Report は gaps の結果。
type Report struct {
	Summary   Summary           `json:"summary"`
	Fields    []FieldGap        `json:"fields"`
	Links     []LinkGap         `json:"links"`
	Questions []QuestionSummary `json:"questions"`
	Unused    []Unused          `json:"unused_sources"`
}

// Build は gaps を計算する。
func Build(p *project.Project, blockingOnly bool) *Report {
	r := &Report{Summary: Summary{Elements: map[string]int{}}}
	g := graph.Build(p)
	for _, id := range p.Order {
		e := p.Elements[id]
		r.Summary.Elements[e.Type]++
		td := p.Schema.Types[e.Type]
		blocked := false
		for _, name := range e.FieldOrder {
			f := e.Fields[name]
			r.Summary.Fields++
			lbl, blk := name, false
			if td != nil {
				if d := td.Fields[name]; d != nil {
					lbl, blk = d.Label, d.Blocking
				}
			}
			switch f.Status {
			case model.StatusConfirmed:
				r.Summary.Confirmed++
				continue
			case model.StatusNA:
				r.Summary.NotApplicable++
				continue
			case model.StatusUnknown:
				r.Summary.Unknown++
			case model.StatusConflict:
				r.Summary.Conflict++
			default:
				continue
			}
			if blk {
				blocked = true
				r.Summary.BlockingGaps++
			}
			if blockingOnly && !blk {
				continue
			}
			r.Fields = append(r.Fields, FieldGap{e.ID, e.Type, name, lbl, f.Status, blk, f.Question})
		}
		if blocked {
			r.Summary.BlockedElements++
		} else {
			r.Summary.ReadyElements++
		}
		if td == nil || blockingOnly {
			continue
		}
		for lt, rule := range td.RequiredLinks {
			have := 0
			for _, ed := range g.Out[e.ID] {
				if ed.Type == lt && typeOK(p, ed.To, rule.Types) {
					have++
				}
			}
			if have < rule.Min {
				r.Links = append(r.Links, LinkGap{e.ID, "missing_link", lt, rule.Min, have,
					fmt.Sprintf("%s から %s（%s）へのリンクが不足", e.ID, lt, strings.Join(rule.Types, "/"))})
			}
		}
		for lt, rule := range td.RequiredInbound {
			have := 0
			for _, ed := range g.In[e.ID] {
				if ed.Type == lt && typeOK(p, ed.From, rule.Types) {
					have++
				}
			}
			if have < rule.Min {
				r.Links = append(r.Links, LinkGap{e.ID, "missing_inbound", lt, rule.Min, have,
					fmt.Sprintf("%s を %s で参照する %s がありません（実現・検証されていない可能性）", e.ID, lt, strings.Join(rule.Types, "/"))})
			}
		}
	}
	sort.SliceStable(r.Links, func(i, j int) bool {
		if r.Links[i].Element != r.Links[j].Element {
			return r.Links[i].Element < r.Links[j].Element
		}
		return r.Links[i].LinkType < r.Links[j].LinkType
	})
	r.Summary.LinkGaps = len(r.Links)
	if r.Summary.Fields > 0 {
		r.Summary.ConfirmedRate = float64(r.Summary.Confirmed+r.Summary.NotApplicable) / float64(r.Summary.Fields)
	}
	r.Summary.SourceUsed, r.Summary.SourceAnchors, r.Unused = SourceCoverage(p)
	for _, q := range p.Questions {
		if q.Status != "open" {
			continue
		}
		r.Summary.OpenQuestions++
		if blockingOnly && !q.Blocking {
			continue
		}
		r.Questions = append(r.Questions, QuestionSummary{q.ID, q.Blocking, q.To, q.AsksFor, q.Status, q.Context})
	}
	sort.Slice(r.Questions, func(i, j int) bool {
		if r.Questions[i].Blocking != r.Questions[j].Blocking {
			return r.Questions[i].Blocking
		}
		return r.Questions[i].ID < r.Questions[j].ID
	})
	return r
}

func typeOK(p *project.Project, id string, types []string) bool {
	if len(types) == 0 {
		return true
	}
	e, ok := p.Elements[id]
	if !ok {
		return false
	}
	for _, t := range types {
		if e.Type == t {
			return true
		}
	}
	return false
}

// Text は人が読む形にする。
func (r *Report) Text() string {
	var b strings.Builder
	s := r.Summary
	var els []string
	for t, n := range s.Elements {
		els = append(els, fmt.Sprintf("%s %d", t, n))
	}
	sort.Strings(els)
	fmt.Fprintf(&b, "要素: %s\n", strings.Join(els, " / "))
	fmt.Fprintf(&b, "項目: %d（確定 %d・該当なし %d・空白 %d・矛盾 %d、確定率 %.0f%%）\n", s.Fields, s.Confirmed, s.NotApplicable, s.Unknown, s.Conflict, s.ConfirmedRate*100)
	fmt.Fprintf(&b, "AI が着手できる要素: %d / 着手条件が欠けている要素: %d（必須の空白・矛盾 %d 件）\n", s.ReadyElements, s.BlockedElements, s.BlockingGaps)
	fmt.Fprintf(&b, "リンク不足: %d / 未回答の質問: %d\n", s.LinkGaps, s.OpenQuestions)
	fmt.Fprintf(&b, "情報源の網羅: %d / %d 箇所が Canon の出典になっている（未使用 %d）\n", s.SourceUsed, s.SourceAnchors, s.SourceAnchors-s.SourceUsed)
	if len(r.Fields) > 0 {
		b.WriteString("\n■ 空白・矛盾\n")
		for _, f := range r.Fields {
			mark := "注意"
			if f.Blocking {
				mark = "必須"
			}
			st := "空白"
			if f.Status == model.StatusConflict {
				st = "矛盾"
			}
			fmt.Fprintf(&b, "  [%s] %s.%s（%s）: %s → %s\n", mark, f.Element, f.Field, f.Label, st, orDash(f.Question))
		}
	}
	if len(r.Links) > 0 {
		b.WriteString("\n■ リンク不足\n")
		for _, l := range r.Links {
			fmt.Fprintf(&b, "  %s\n", l.Message)
		}
	}
	if len(r.Unused) > 0 {
		b.WriteString("\n■ 出典として使われていない情報源の箇所（取りこぼしの可能性）\n")
		for _, u := range r.Unused {
			fmt.Fprintf(&b, "  %s（%s）\n", u.Anchor, u.File)
		}
	}
	if len(r.Questions) > 0 {
		b.WriteString("\n■ 未回答の質問\n")
		for _, q := range r.Questions {
			mark := "注意"
			if q.Blocking {
				mark = "必須"
			}
			fmt.Fprintf(&b, "  [%s] %s（宛先: %s）: %s\n", mark, q.ID, orDash(q.To), strings.Join(q.AsksFor, ", "))
		}
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
