// Package score は取り込み結果を期待結果と比べて採点する（評価用）。
package score

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
)

// Miss は期待と違った1件。
type Miss struct {
	Element  string `json:"element"`
	Field    string `json:"field,omitempty"`
	Kind     string `json:"kind"` // missing_element / missing_field / status / value / source / extra_element
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// Result は採点結果。
type Result struct {
	ExpectedFields int     `json:"expected_fields"`
	StatusOK       int     `json:"status_ok"`
	ValueOK        int     `json:"value_ok"`
	ValueTotal     int     `json:"value_total"`
	SourceOK       int     `json:"source_ok"`
	SourceTotal    int     `json:"source_total"`
	StatusRate     float64 `json:"status_rate"`
	ValueRate      float64 `json:"value_rate"`
	SourceRate     float64 `json:"source_rate"`
	Misses         []Miss  `json:"misses"`
}

// Run は expectedDir と actualDir を比べる。要素は ID、無ければ（型・名前）で対応付ける。
func Run(expectedDir, actualDir string) *Result {
	exp, _, _ := project.LoadDocs(expectedDir)
	act, _, _ := project.LoadDocs(actualDir)
	byName := map[string]*model.Element{}
	for _, a := range act {
		byName[a.Type+"|"+a.Name] = a
	}
	used := map[*model.Element]bool{}
	r := &Result{}
	ids := make([]string, 0, len(exp))
	for id := range exp {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := exp[id]
		a := act[id]
		if a == nil {
			a = byName[e.Type+"|"+e.Name]
		}
		if a == nil {
			r.ExpectedFields += len(e.FieldOrder)
			r.Misses = append(r.Misses, Miss{Element: e.ID, Kind: "missing_element", Expected: e.Name})
			continue
		}
		used[a] = true
		for _, fn := range e.FieldOrder {
			ef := e.Fields[fn]
			r.ExpectedFields++
			af, ok := a.Fields[fn]
			if !ok {
				r.Misses = append(r.Misses, Miss{Element: e.ID, Field: fn, Kind: "missing_field"})
				continue
			}
			if ef.Status == af.Status {
				r.StatusOK++
			} else {
				r.Misses = append(r.Misses, Miss{e.ID, fn, "status", ef.Status, af.Status})
				continue
			}
			if ef.Status == model.StatusConfirmed {
				r.ValueTotal++
				if model.Normalize(ef.Value) == model.Normalize(af.Value) {
					r.ValueOK++
				} else {
					r.Misses = append(r.Misses, Miss{e.ID, fn, "value", model.Normalize(ef.Value), model.Normalize(af.Value)})
				}
				r.SourceTotal++
				if sameSource(ef.Source, af.Source) {
					r.SourceOK++
				} else {
					r.Misses = append(r.Misses, Miss{e.ID, fn, "source", ef.Source.String(), af.Source.String()})
				}
			}
		}
	}
	for _, a := range act {
		if !used[a] {
			r.Misses = append(r.Misses, Miss{Element: a.ID, Kind: "extra_element", Actual: a.Name})
		}
	}
	if r.ExpectedFields > 0 {
		r.StatusRate = float64(r.StatusOK) / float64(r.ExpectedFields)
	}
	if r.ValueTotal > 0 {
		r.ValueRate = float64(r.ValueOK) / float64(r.ValueTotal)
	}
	if r.SourceTotal > 0 {
		r.SourceRate = float64(r.SourceOK) / float64(r.SourceTotal)
	}
	return r
}

func sameSource(a, b *model.Source) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case "answer":
		return a.Question == b.Question
	case "derived":
		return a.Rule == b.Rule
	default:
		return model.NormalizeRef(a.Ref) == model.NormalizeRef(b.Ref)
	}
}

// Text は人が読む形にする。
func (r *Result) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "状態の一致: %d/%d（%.0f%%）\n", r.StatusOK, r.ExpectedFields, r.StatusRate*100)
	fmt.Fprintf(&b, "値の一致  : %d/%d（%.0f%%）\n", r.ValueOK, r.ValueTotal, r.ValueRate*100)
	fmt.Fprintf(&b, "出典の一致: %d/%d（%.0f%%）\n", r.SourceOK, r.SourceTotal, r.SourceRate*100)
	for _, m := range r.Misses {
		where := m.Element
		if m.Field != "" {
			where += "." + m.Field
		}
		fmt.Fprintf(&b, "  [%s] %s", m.Kind, where)
		if m.Expected != "" || m.Actual != "" {
			fmt.Fprintf(&b, "\n      期待: %s\n      結果: %s", m.Expected, m.Actual)
		}
		b.WriteString("\n")
	}
	return b.String()
}
