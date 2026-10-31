// Package graph は要素間のリンクをたどる。
package graph

import (
	"sort"

	"github.com/bubu-xx/canon/tools/internal/project"
)

// Edge は1本のリンク。
type Edge struct {
	From string `json:"from"`
	Type string `json:"type"`
	To   string `json:"to"`
}

// Graph は要素のリンク集合。
type Graph struct {
	Out map[string][]Edge
	In  map[string][]Edge
}

// Build は Project からグラフを作る。
func Build(p *project.Project) *Graph {
	g := &Graph{Out: map[string][]Edge{}, In: map[string][]Edge{}}
	for _, id := range p.Order {
		e := p.Elements[id]
		for _, l := range e.Links {
			ed := Edge{From: e.ID, Type: l.Type, To: l.Target}
			g.Out[e.ID] = append(g.Out[e.ID], ed)
			g.In[l.Target] = append(g.In[l.Target], ed)
		}
	}
	return g
}

// Related は id と直接つながる要素（両方向）。
func (g *Graph) Related(id string) []string {
	seen := map[string]bool{}
	for _, e := range g.Out[id] {
		seen[e.To] = true
	}
	for _, e := range g.In[id] {
		seen[e.From] = true
	}
	delete(seen, id)
	return keys(seen)
}

// ImpactItem は影響を受ける要素と、そこへ至る経路。
type ImpactItem struct {
	ID   string   `json:"id"`
	Path []string `json:"path"`
}

// Impact は id を変えたときに見直しが要る要素。
// id を参照している要素（derived_from・verifies・uses 等で id を指すもの）を推移的にたどり、
// あわせて id 自身が verified_by で指す試験も含める。
func (g *Graph) Impact(id string) []ImpactItem {
	var out []ImpactItem
	seen := map[string]bool{id: true}
	type node struct {
		id   string
		path []string
	}
	queue := []node{{id, []string{id}}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		var next []Edge
		next = append(next, g.In[n.id]...)
		for _, e := range g.Out[n.id] {
			if e.Type == "verified_by" {
				next = append(next, Edge{From: e.To, Type: "verifies", To: n.id})
			}
		}
		for _, e := range next {
			if seen[e.From] {
				continue
			}
			seen[e.From] = true
			p := append(append([]string{}, n.path...), e.Type+"←"+e.From)
			out = append(out, ImpactItem{ID: e.From, Path: p})
			queue = append(queue, node{e.From, append(append([]string{}, n.path...), e.From)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
