// Package model は Canon の要素・フィールド・出典・リンク・質問を表す。
package model

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	StatusConfirmed = "confirmed"
	StatusUnknown   = "unknown"
	StatusConflict  = "conflict"
	StatusNA        = "not_applicable"
)

// Source は値やリンクの根拠。
type Source struct {
	Kind     string   `json:"kind"`
	Ref      string   `json:"ref,omitempty"`
	Commit   string   `json:"commit,omitempty"`
	By       string   `json:"by,omitempty"`
	At       string   `json:"at,omitempty"`
	Question string   `json:"question,omitempty"`
	From     []string `json:"from,omitempty"`
	Rule     string   `json:"rule,omitempty"`
	Keys     []string `json:"-"`
}

// String は人が読む形の出典。
func (s *Source) String() string {
	if s == nil {
		return ""
	}
	switch s.Kind {
	case "document":
		return s.Ref
	case "code":
		if s.Commit != "" {
			return fmt.Sprintf("%s (%s)", s.Ref, s.Commit)
		}
		return s.Ref
	case "answer":
		return fmt.Sprintf("%s の回答 %s（%s）", s.By, s.At, s.Question)
	case "derived":
		return fmt.Sprintf("%s から算出（%s）", strings.Join(s.From, ", "), s.Rule)
	}
	return s.Kind
}

// Claim は矛盾時の各情報源の主張。
type Claim struct {
	Value  any     `json:"value"`
	Source *Source `json:"source,omitempty"`
}

// Field は要素の1項目。出典と状態はフィールド単位で持つ。
type Field struct {
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Value    any       `json:"value,omitempty"`
	HasValue bool      `json:"-"`
	Unit     string    `json:"unit,omitempty"`
	Source   *Source   `json:"source,omitempty"`
	Also     []*Source `json:"also,omitempty"` // 同じ値を述べている他の情報源（裏付け）
	Claims   []Claim   `json:"claims,omitempty"`
	Question string    `json:"question,omitempty"`
	Keys     []string  `json:"-"`
	Line     int       `json:"-"`
}

// Link は型付きの関係。
type Link struct {
	Type   string  `json:"type"`
	Target string  `json:"target"`
	Source *Source `json:"source,omitempty"`
	Line   int     `json:"-"`
}

// Element は Canon の1要素。
type Element struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Owner      string            `json:"owner,omitempty"`
	Fields     map[string]*Field `json:"fields"`
	FieldOrder []string          `json:"-"`
	Links      []Link            `json:"links,omitempty"`
	File       string            `json:"file"`
	Keys       []string          `json:"-"`
	Problems   []string          `json:"-"` // 読み込み時に見つかった形の問題
}

// Question は空白・矛盾を埋めるための質問。
type Question struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	AsksFor  []string       `json:"asks_for"`
	Blocking bool           `json:"blocking"`
	To       string         `json:"to,omitempty"`
	Context  string         `json:"context,omitempty"`
	Options  []any          `json:"options,omitempty"`
	Status   string         `json:"status"`
	Answer   map[string]any `json:"answer,omitempty"`
	File     string         `json:"file"`
	Keys     []string       `json:"-"`
	Problems []string       `json:"-"`
}

// ParseDoc は YAML 文書を読み、type を見て Element か Question を返す。
func ParseDoc(data []byte, file string) (*Element, *Question, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, fmt.Errorf("YAML として読めません: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, nil, fmt.Errorf("空のファイルです")
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("最上位はマッピング（キー: 値）である必要があります")
	}
	if str(get(doc, "type")) == "question" {
		return nil, parseQuestion(doc, file), nil
	}
	return parseElement(doc, file), nil, nil
}

func parseElement(doc *yaml.Node, file string) *Element {
	e := &Element{File: file, Fields: map[string]*Field{}}
	e.Keys = keys(doc)
	e.ID = str(get(doc, "id"))
	e.Type = str(get(doc, "type"))
	e.Name = str(get(doc, "name"))
	e.Owner = str(get(doc, "owner"))
	if fn := get(doc, "fields"); fn != nil {
		if fn.Kind != yaml.MappingNode {
			e.Problems = append(e.Problems, "fields はマッピングである必要があります")
		} else {
			for i := 0; i+1 < len(fn.Content); i += 2 {
				name := fn.Content[i].Value
				f := parseField(name, fn.Content[i+1])
				e.Fields[name] = f
				e.FieldOrder = append(e.FieldOrder, name)
			}
		}
	}
	if ln := get(doc, "links"); ln != nil {
		if ln.Kind != yaml.SequenceNode {
			e.Problems = append(e.Problems, "links はリストである必要があります")
		} else {
			for _, it := range ln.Content {
				l := Link{Type: str(get(it, "type")), Target: str(get(it, "target")), Line: it.Line}
				l.Source = parseSource(get(it, "source"))
				e.Links = append(e.Links, l)
			}
		}
	}
	return e
}

func parseField(name string, n *yaml.Node) *Field {
	f := &Field{Name: name, Line: n.Line}
	if n.Kind != yaml.MappingNode {
		f.Keys = nil
		return f
	}
	f.Keys = keys(n)
	f.Status = str(get(n, "status"))
	if v := get(n, "value"); v != nil {
		f.HasValue = true
		f.Value = decode(v)
	}
	f.Unit = str(get(n, "unit"))
	f.Source = parseSource(get(n, "source"))
	if an := get(n, "also"); an != nil && an.Kind == yaml.SequenceNode {
		for _, x := range an.Content {
			if s := parseSource(x); s != nil {
				f.Also = append(f.Also, s)
			}
		}
	}
	f.Question = str(get(n, "question"))
	if cn := get(n, "claims"); cn != nil && cn.Kind == yaml.SequenceNode {
		for _, c := range cn.Content {
			cl := Claim{Source: parseSource(get(c, "source"))}
			if v := get(c, "value"); v != nil {
				cl.Value = decode(v)
			}
			f.Claims = append(f.Claims, cl)
		}
	}
	return f
}

func parseSource(n *yaml.Node) *Source {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	s := &Source{Keys: keys(n)}
	s.Kind = str(get(n, "kind"))
	s.Ref = str(get(n, "ref"))
	s.Commit = str(get(n, "commit"))
	s.By = str(get(n, "by"))
	s.At = str(get(n, "at"))
	s.Question = str(get(n, "question"))
	s.Rule = str(get(n, "rule"))
	if fr := get(n, "from"); fr != nil {
		for _, x := range fr.Content {
			s.From = append(s.From, x.Value)
		}
	}
	return s
}

func parseQuestion(doc *yaml.Node, file string) *Question {
	q := &Question{File: file, Keys: keys(doc)}
	q.ID = str(get(doc, "id"))
	q.Type = "question"
	q.To = str(get(doc, "to"))
	q.Context = str(get(doc, "context"))
	q.Status = str(get(doc, "status"))
	if b := get(doc, "blocking"); b != nil {
		q.Blocking = b.Value == "true"
	}
	if a := get(doc, "asks_for"); a != nil {
		for _, x := range a.Content {
			q.AsksFor = append(q.AsksFor, x.Value)
		}
	}
	if o := get(doc, "options"); o != nil {
		if v, ok := decode(o).([]any); ok {
			q.Options = v
		}
	}
	if a := get(doc, "answer"); a != nil {
		if m, ok := decode(a).(map[string]any); ok {
			q.Answer = m
		}
	}
	return q
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

func str(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func decode(n *yaml.Node) any {
	var v any
	_ = n.Decode(&v)
	return v
}

// Normalize は値の比較用に正規化した文字列を返す。
func Normalize(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.Join(strings.Fields(t), " ")
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = Normalize(x)
		}
		return "[" + strings.Join(parts, " | ") + "]"
	case map[string]any:
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// NormalizeRef は出典参照の比較用正規化。
func NormalizeRef(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
