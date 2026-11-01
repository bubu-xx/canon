// Package schema は標準スキーマ（期待構造）とプロジェクト拡張を読み込む。
package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FieldDef はフィールドの定義。
type FieldDef struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Values   []string `json:"values,omitempty"`
	Units    []string `json:"units,omitempty"`
	Required bool     `json:"required"`
	// RequiredWhen は条件付きの必須。他の項目が確定していて値がこの中にあれば必須（Required が false でも）
	RequiredWhen map[string][]string `json:"required_when,omitempty"`
	Blocking     bool                `json:"blocking"`
	Description  string              `json:"description,omitempty"`
}

// LinkRule は必須リンクの定義。
type LinkRule struct {
	Min   int      `json:"min"`
	Types []string `json:"types"` // target_types または from_types
}

// TypeDef は要素の型の定義。
type TypeDef struct {
	Name            string               `json:"name"`
	Prefix          string               `json:"prefix"`
	Label           string               `json:"label"`
	Fields          map[string]*FieldDef `json:"fields"`
	FieldOrder      []string             `json:"field_order"`
	RequiredLinks   map[string]LinkRule  `json:"required_links"`
	RequiredInbound map[string]LinkRule  `json:"required_inbound"`
	MaxElements     int                  `json:"max_elements,omitempty"` // 0 は上限なし
}

// Schema はスキーマ全体。
type Schema struct {
	Dir          string              `json:"dir"`
	Profile      string              `json:"profile"`
	ProfileLabel string              `json:"profile_label"`
	Types        map[string]*TypeDef `json:"types"`
	TypeOrder    []string            `json:"type_order"`
	LinkTypes    []string            `json:"link_types"`
}

// Load は標準スキーマ dir と、あればプロジェクト拡張 extDir を読む（プロファイルは既定）。
func Load(dir, extDir string) (*Schema, error) {
	return LoadWithProfile(dir, extDir, "")
}

// LoadWithProfile はプロファイル（profiles/<name>.yaml）を適用して読む。空なら business。
func LoadWithProfile(dir, extDir, profile string) (*Schema, error) {
	s := &Schema{Dir: dir, Types: map[string]*TypeDef{}}
	if err := s.loadCommon(filepath.Join(dir, "common.yaml")); err != nil {
		return nil, err
	}
	if err := s.loadTypes(filepath.Join(dir, "types")); err != nil {
		return nil, err
	}
	if extDir != "" {
		if _, err := os.Stat(filepath.Join(extDir, "types")); err == nil {
			if err := s.loadTypes(filepath.Join(extDir, "types")); err != nil {
				return nil, fmt.Errorf("schema-ext: %w", err)
			}
		}
	}
	if profile == "" {
		profile = "business"
	}
	if err := s.applyProfile(filepath.Join(dir, "profiles", profile+".yaml")); err != nil {
		return nil, err
	}
	s.Profile = profile
	// 設計書の流れ（全体 → 要求 → 外部から見えるもの → 振る舞い → 構成・判断 → 用語 → 試験）の順に並べる
	order := []string{"prj", "req", "scr", "rpt", "ifc", "dat", "bhv", "cmp", "adr", "term", "tst"}
	seen := map[string]bool{}
	for _, t := range order {
		if _, ok := s.Types[t]; ok {
			s.TypeOrder = append(s.TypeOrder, t)
			seen[t] = true
		}
	}
	var rest []string
	for t := range s.Types {
		if !seen[t] {
			rest = append(rest, t)
		}
	}
	sort.Strings(rest)
	s.TypeOrder = append(s.TypeOrder, rest...)
	return s, nil
}

func (s *Schema) loadCommon(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("標準スキーマが読めません（%s）: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	link := get(root.Content[0], "link")
	for _, k := range keys(get(link, "types")) {
		s.LinkTypes = append(s.LinkTypes, k)
	}
	return nil
}

func (s *Schema) loadTypes(dir string) error {
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		doc := root.Content[0]
		name := get(doc, "type").Value
		if name == "question" {
			continue // 質問は値の型ではない
		}
		td, ok := s.Types[name]
		if !ok {
			td = &TypeDef{Name: name, Fields: map[string]*FieldDef{}, RequiredLinks: map[string]LinkRule{}, RequiredInbound: map[string]LinkRule{}}
			s.Types[name] = td
		}
		if v := val(get(doc, "id_prefix")); v != "" {
			td.Prefix = v
		}
		if v := val(get(doc, "label")); v != "" {
			td.Label = v
		}
		if v := val(get(doc, "max_elements")); v != "" {
			fmt.Sscanf(v, "%d", &td.MaxElements)
		}
		fields := get(doc, "fields")
		for i := 0; fields != nil && i+1 < len(fields.Content); i += 2 {
			fname := fields.Content[i].Value
			fn := fields.Content[i+1]
			fd := &FieldDef{Name: fname}
			fd.Label = val(get(fn, "label"))
			fd.Type = val(get(fn, "type"))
			fd.Required = val(get(fn, "required")) == "true"
			fd.Blocking = val(get(fn, "blocking")) == "true"
			fd.Description = val(get(fn, "description"))
			fd.Values = list(get(fn, "values"))
			fd.Units = list(get(fn, "units"))
			if rw := get(fn, "required_when"); rw != nil {
				fd.RequiredWhen = map[string][]string{}
				for k := 0; k+1 < len(rw.Content); k += 2 {
					fd.RequiredWhen[rw.Content[k].Value] = list(rw.Content[k+1])
				}
			}
			if fd.Label == "" {
				fd.Label = fname
			}
			if _, exists := td.Fields[fname]; !exists {
				td.FieldOrder = append(td.FieldOrder, fname)
			}
			td.Fields[fname] = fd
		}
		links := get(doc, "links")
		for lt, rule := range rules(get(links, "required"), "target_types") {
			td.RequiredLinks[lt] = rule
		}
		for lt, rule := range rules(get(links, "required_inbound"), "from_types") {
			td.RequiredInbound[lt] = rule
		}
	}
	return nil
}

func (s *Schema) applyProfile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("プロファイルが見つかりません（%s）。profiles/ にあるもの（business / library など）を指定してください", path)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	doc := root.Content[0]
	s.ProfileLabel = val(get(doc, "label"))
	types := get(doc, "types")
	for i := 0; types != nil && i+1 < len(types.Content); i += 2 {
		td := s.Types[types.Content[i].Value]
		if td == nil {
			continue
		}
		fields := get(types.Content[i+1], "fields")
		for j := 0; fields != nil && j+1 < len(fields.Content); j += 2 {
			fd := td.Fields[fields.Content[j].Value]
			if fd == nil {
				continue
			}
			o := fields.Content[j+1]
			if v := get(o, "required"); v != nil {
				fd.Required = v.Value == "true"
			}
			if v := get(o, "blocking"); v != nil {
				fd.Blocking = v.Value == "true"
			}
		}
	}
	return nil
}

// TypeByPrefix は ID の接頭辞から型を引く。
func (s *Schema) TypeByPrefix(id string) *TypeDef {
	p := id
	if i := strings.Index(id, "-"); i > 0 {
		p = id[:i]
	}
	for _, t := range s.Types {
		if t.Prefix == p {
			return t
		}
	}
	return nil
}

// HasLinkType は定義済みのリンク種別か。
func (s *Schema) HasLinkType(t string) bool {
	for _, x := range s.LinkTypes {
		if x == t {
			return true
		}
	}
	return false
}

func rules(n *yaml.Node, typesKey string) map[string]LinkRule {
	out := map[string]LinkRule{}
	for i := 0; n != nil && i+1 < len(n.Content); i += 2 {
		r := LinkRule{Min: 1}
		var min int
		fmt.Sscanf(val(get(n.Content[i+1], "min")), "%d", &min)
		if min > 0 {
			r.Min = min
		}
		r.Types = list(get(n.Content[i+1], typesKey))
		out[n.Content[i].Value] = r
	}
	return out
}

func get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func keys(n *yaml.Node) []string {
	var out []string
	if n == nil || n.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, n.Content[i].Value)
	}
	return out
}

func val(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func list(n *yaml.Node) []string {
	var out []string
	if n == nil {
		return out
	}
	for _, x := range n.Content {
		out = append(out, x.Value)
	}
	return out
}
