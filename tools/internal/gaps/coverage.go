package gaps

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
	"go.yaml.in/yaml/v3"
)

// Unused は Canon のどこからも出典として使われていない情報源の箇所。
type Unused struct {
	Anchor string `json:"anchor"`
	File   string `json:"file"`
}

// Ignored は人が「対象外」と判断した情報源の箇所（.canon/coverage.yaml）。
type Ignored struct {
	Anchor string `yaml:"anchor"`
	Reason string `yaml:"reason"`
	By     string `yaml:"by"`
}

// SourceCoverage は情報源の網羅率。すべてのアンカーが、出典として使われているか、人が対象外と判断しているかを調べる。
func SourceCoverage(p *project.Project) (used, total int, unused []Unused) {
	refs := map[string]bool{}
	mark := func(s *model.Source) {
		if s != nil && s.Kind == "document" {
			refs[model.NormalizeRef(s.Ref)] = true
		}
	}
	for _, e := range p.Elements {
		for _, f := range e.Fields {
			mark(f.Source)
			for _, s := range f.Also {
				mark(s)
			}
			for _, c := range f.Claims {
				mark(c.Source)
			}
		}
		for _, l := range e.Links {
			mark(l.Source)
		}
	}
	var ig struct {
		Ignored []Ignored `yaml:"ignored"`
	}
	if data, err := os.ReadFile(filepath.Join(p.Root, project.Dir, "coverage.yaml")); err == nil {
		_ = yaml.Unmarshal(data, &ig)
	}
	for _, x := range ig.Ignored {
		if x.By != "" {
			refs[model.NormalizeRef(x.Anchor)] = true
		}
	}
	for a, file := range p.Anchors {
		total++
		if refs[a] {
			used++
			continue
		}
		unused = append(unused, Unused{Anchor: a, File: filepath.Base(file)})
	}
	sort.Slice(unused, func(i, j int) bool { return unused[i].Anchor < unused[j].Anchor })
	return
}
