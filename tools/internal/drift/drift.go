// Package drift は観測（実装の状態）と Canon（あるべき姿）のズレを出す。
package drift

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
)

// Item は1件のズレ。
type Item struct {
	Kind    string `json:"kind"` // value_mismatch / impl_detail / impl_only / canon_only / conflict_open / unmapped_code / stale / unknown_element
	Element string `json:"element,omitempty"`
	Field   string `json:"field,omitempty"`
	Canon   string `json:"canon,omitempty"`
	Impl    string `json:"impl,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message"`
}

// Report は drift の結果。
type Report struct {
	Head             string  `json:"head,omitempty"`
	Items            []Item  `json:"items"`
	ObservedElements int     `json:"observed_elements"`
	Elements         int     `json:"elements"`
	Coverage         float64 `json:"coverage"`
	Unmapped         int     `json:"unmapped"`
}

// Build は drift を計算する。head は現在のコミット（空なら stale を判定しない）。
// types は観測の対象にする型（空なら bhv のみ）。
func Build(p *project.Project, head string, types []string) *Report {
	if len(types) == 0 {
		types = []string{"bhv"}
	}
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	r := &Report{Head: head}
	for _, id := range p.Order {
		e := p.Elements[id]
		if !want[e.Type] {
			continue
		}
		r.Elements++
		o, ok := p.Observations[e.ID]
		if !ok {
			continue
		}
		r.ObservedElements++
		if head != "" && o.Commit != "" && !sameCommit(o.Commit, head) {
			r.Items = append(r.Items, Item{Kind: "stale", Element: e.ID, Message: fmt.Sprintf("観測が古い（観測 %s／現在 %s）。再観測してください", o.Commit, head)})
		}
		names := make([]string, 0, len(o.Fields))
		for n := range o.Fields {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			of := o.Fields[n]
			cf, has := e.Fields[n]
			impl := model.Normalize(of.Value)
			if has && cf.Status == model.StatusNA {
				continue // 該当なしと判断済み
			}
			switch {
			case !has || cf.Status == model.StatusUnknown:
				r.Items = append(r.Items, Item{Kind: "impl_only", Element: e.ID, Field: n, Impl: impl, Ref: of.Ref,
					Message: "コードにはあるが Canon は未確定。質問の候補（コードの値を意図として採るか）"})
			case cf.Status == model.StatusConflict:
				match := ""
				for _, cl := range cf.Claims {
					if model.Normalize(cl.Value) == impl {
						match = cl.Source.String()
					}
				}
				msg := "Canon は矛盾のまま（" + cf.Question + "）。実装はどの主張とも一致しない"
				if match != "" {
					msg = "Canon は矛盾のまま（" + cf.Question + "）。実装は「" + match + "」の主張と一致"
				}
				r.Items = append(r.Items, Item{Kind: "conflict_open", Element: e.ID, Field: n, Impl: impl, Ref: of.Ref, Message: msg})
			case cf.Status == model.StatusConfirmed && model.Normalize(cf.Value) != impl && covers(of.Value, cf.Value):
				r.Items = append(r.Items, Item{Kind: "impl_detail", Element: e.ID, Field: n, Canon: model.Normalize(cf.Value), Impl: impl, Ref: of.Ref,
					Message: "Canon の内容はすべて実装にあり、実装が詳細を足している。足した部分は Canon に無い（質問か情報源の追記の候補）"})
			case cf.Status == model.StatusConfirmed && model.Normalize(cf.Value) != impl:
				r.Items = append(r.Items, Item{Kind: "value_mismatch", Element: e.ID, Field: n, Canon: model.Normalize(cf.Value), Impl: impl, Ref: of.Ref,
					Message: "Canon と実装が違う。バグか、Canon への未反映の変更"})
			}
		}
		if o.Complete {
			for _, n := range e.FieldOrder {
				cf := e.Fields[n]
				if _, seen := o.Fields[n]; !seen && cf.Status == model.StatusConfirmed {
					r.Items = append(r.Items, Item{Kind: "canon_only", Element: e.ID, Field: n, Canon: model.Normalize(cf.Value),
						Message: "Canon にあるが実装に見当たらない（未実装の可能性）"})
				}
			}
		}
	}
	for id := range p.Observations {
		if _, ok := p.Elements[id]; !ok {
			r.Items = append(r.Items, Item{Kind: "unknown_element", Element: id, Message: "観測の対象要素が Canon にありません"})
		}
	}
	if p.Unmapped != nil {
		for _, en := range p.Unmapped.Entries {
			r.Unmapped++
			r.Items = append(r.Items, Item{Kind: "unmapped_code", Ref: en.Ref, Message: "どの要素にも対応付けられていないコード: " + en.Note})
		}
	}
	if r.Elements > 0 {
		r.Coverage = float64(r.ObservedElements) / float64(r.Elements)
	}
	return r
}

// covers は実装の値 impl が Canon の値 canon をすべて含むか（詳細の追加だけか）。
func covers(impl, canon any) bool {
	ci := items(canon)
	ii := items(impl)
	if len(ci) == 0 {
		return false
	}
	for _, c := range ci {
		found := false
		for _, x := range ii {
			if x == c || strings.Contains(x, c) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func items(v any) []string {
	if xs, ok := v.([]any); ok {
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			out = append(out, model.Normalize(x))
		}
		return out
	}
	if v == nil {
		return nil
	}
	return []string{model.Normalize(v)}
}

func sameCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// Text は人が読む形にする。
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "観測済み: %d / %d 要素（カバレッジ %.0f%%）、対応付けのないコード: %d\n", r.ObservedElements, r.Elements, r.Coverage*100, r.Unmapped)
	if r.Elements > r.ObservedElements {
		fmt.Fprintf(&b, "※ 観測していない %d 要素はズレが見えない領域です\n", r.Elements-r.ObservedElements)
	}
	for _, it := range r.Items {
		where := it.Element
		if it.Field != "" {
			where += "." + it.Field
		}
		if where == "" {
			where = it.Ref
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", it.Kind, where, it.Message)
		if it.Canon != "" || it.Impl != "" {
			fmt.Fprintf(&b, "      Canon: %s\n      実装 : %s %s\n", orDash(it.Canon), orDash(it.Impl), paren(it.Ref))
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

func paren(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}
