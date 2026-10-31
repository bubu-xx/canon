// Package project は対象プロジェクトの .canon/ を見つけて読み込む。
package project

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/schema"
	"go.yaml.in/yaml/v3"
)

const Dir = ".canon"

// ErrNoCanon は .canon/ が見つからないことを表す。
var ErrNoCanon = errors.New(".canon/ が見つかりません（/canon:canon-init で作成してください）")

// Observation はコードから観測した実装の状態。
type Observation struct {
	Element    string                   `yaml:"element" json:"element"`
	Commit     string                   `yaml:"commit" json:"commit"`
	ObservedAt string                   `yaml:"observed_at" json:"observed_at"`
	Complete   bool                     `yaml:"complete" json:"complete"`
	Fields     map[string]ObservedField `yaml:"fields" json:"fields"`
	File       string                   `yaml:"-" json:"file"`
}

// ObservedField は観測した値とその位置。
type ObservedField struct {
	Value any    `yaml:"value" json:"value"`
	Ref   string `yaml:"ref" json:"ref"`
}

// Unmapped はどの要素にも対応付けられていないコード。
type Unmapped struct {
	Commit  string `yaml:"commit" json:"commit"`
	Entries []struct {
		Ref  string `yaml:"ref" json:"ref"`
		Note string `yaml:"note" json:"note"`
	} `yaml:"entries" json:"entries"`
}

// Project は読み込んだ対象プロジェクト。
type Project struct {
	Root         string
	Config       Config
	Schema       *schema.Schema
	Elements     map[string]*model.Element
	Questions    map[string]*model.Question
	Order        []string          // 要素 ID の並び（型順→ID順）
	Anchors      map[string]string // 正規化した出典参照 → 情報源ファイル
	Observations map[string]*Observation
	Unmapped     *Unmapped
	LoadErrors   []FileError
}

// Config は .canon/config.yaml。
type Config struct {
	Project        string            `yaml:"project" json:"project"`
	Profile        string            `yaml:"profile" json:"profile"`
	Scope          []string          `yaml:"scope" json:"scope"`
	QuestionOwners map[string]string `yaml:"question_owners" json:"question_owners"`
}

// ReadConfig は root の .canon/config.yaml を読む（無ければ空）。
func ReadConfig(root string) Config {
	var c Config
	if data, err := os.ReadFile(filepath.Join(root, Dir, "config.yaml")); err == nil {
		_ = yaml.Unmarshal(data, &c)
	}
	if c.Project == "" {
		c.Project = filepath.Base(root)
	}
	return c
}

// FileError は読めなかったファイル。
type FileError struct {
	File string
	Err  error
}

// FindRoot は start から上へ .canon/ を探す。
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, Dir)); err == nil && st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoCanon
		}
		dir = parent
	}
}

// SchemaDir は標準スキーマの場所を決める。
// 優先順：引数 > 環境変数 CANON_SCHEMA > 実行ファイルの ../schema。
func SchemaDir(flag string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv("CANON_SCHEMA"); v != "" {
		return v
	}
	if v := os.Getenv("CLAUDE_PLUGIN_ROOT"); v != "" {
		if st, err := os.Stat(filepath.Join(v, "schema")); err == nil && st.IsDir() {
			return filepath.Join(v, "schema")
		}
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		cand := filepath.Join(filepath.Dir(exe), "..", "schema")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
	}
	return "schema"
}

// Load は root の .canon/ を読む。
func Load(root, schemaDir string) (*Project, error) {
	cfg := ReadConfig(root)
	sc, err := schema.LoadWithProfile(schemaDir, filepath.Join(root, Dir, "schema-ext"), cfg.Profile)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: root, Schema: sc, Config: cfg}
	p.Elements, p.Questions, p.LoadErrors = LoadDocs(filepath.Join(root, Dir, "canon"))
	_, qs, qerrs := LoadDocs(filepath.Join(root, Dir, "questions"))
	for k, v := range qs {
		p.Questions[k] = v
	}
	p.LoadErrors = append(p.LoadErrors, qerrs...)
	p.Anchors = LoadAnchors(filepath.Join(root, Dir, "sources"))
	p.loadObservations()
	p.Reorder()
	return p, nil
}

// LoadDocs は dir 以下の YAML をすべて読む（要素と質問を振り分ける）。
func LoadDocs(dir string) (map[string]*model.Element, map[string]*model.Question, []FileError) {
	els := map[string]*model.Element{}
	qs := map[string]*model.Question{}
	var errs []FileError
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isYAML(path) {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			errs = append(errs, FileError{path, rerr})
			return nil
		}
		e, q, perr := model.ParseDoc(data, path)
		if perr != nil {
			errs = append(errs, FileError{path, perr})
			return nil
		}
		if e != nil {
			key := e.ID
			if key == "" {
				key = "?" + path
			}
			if old, dup := els[key]; dup {
				e.Problems = append(e.Problems, fmt.Sprintf("ID %s が %s と重複しています", e.ID, old.File))
				key = key + "#" + path
			}
			els[key] = e
		}
		if q != nil {
			key := q.ID
			if key == "" {
				key = "?" + path
			}
			if old, dup := qs[key]; dup {
				q.Problems = append(q.Problems, fmt.Sprintf("ID %s が %s と重複しています", q.ID, old.File))
				key = key + "#" + path
			}
			qs[key] = q
		}
		return nil
	})
	return els, qs, errs
}

var anchorRe = regexp.MustCompile(`<!--\s*src:\s*(.+?)\s*-->`)

// LoadAnchors は情報源テキストの出典アンカー（<!-- src: ... -->）を集める。
func LoadAnchors(dir string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			for _, m := range anchorRe.FindAllStringSubmatch(sc.Text(), -1) {
				ref := strings.ReplaceAll(m[1], "|", " ")
				out[model.NormalizeRef(ref)] = path
			}
		}
		return nil
	})
	return out
}

func (p *Project) loadObservations() {
	p.Observations = map[string]*Observation{}
	dir := filepath.Join(p.Root, Dir, "observations")
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isYAML(path) {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), "unmapped.yaml") {
			var u Unmapped
			if yaml.Unmarshal(data, &u) == nil {
				p.Unmapped = &u
			}
			return nil
		}
		var o Observation
		if yerr := yaml.Unmarshal(data, &o); yerr != nil || o.Element == "" {
			p.LoadErrors = append(p.LoadErrors, FileError{path, fmt.Errorf("観測ファイルとして読めません")})
			return nil
		}
		o.File = path
		p.Observations[o.Element] = &o
		return nil
	})
}

// Reorder は要素の並び（型順→ID順）を作り直す。
func (p *Project) Reorder() {
	rank := map[string]int{}
	for i, t := range p.Schema.TypeOrder {
		rank[t] = i
	}
	p.Order = p.Order[:0]
	for id := range p.Elements {
		p.Order = append(p.Order, id)
	}
	sort.Slice(p.Order, func(i, j int) bool {
		a, b := p.Elements[p.Order[i]], p.Elements[p.Order[j]]
		ra, oka := rank[a.Type]
		rb, okb := rank[b.Type]
		if !oka {
			ra = 999
		}
		if !okb {
			rb = 999
		}
		if ra != rb {
			return ra < rb
		}
		return a.ID < b.ID
	})
}

// Override は1ファイルの内容を差し替えた状態にする（書き込み前の検査用）。
// 戻り値は差し替えた要素 ID または質問 ID。
func (p *Project) Override(path string, data []byte) (string, error) {
	e, q, err := model.ParseDoc(data, path)
	if err != nil {
		return "", err
	}
	// 同じファイルの旧版を取り除く
	for k, v := range p.Elements {
		if samePath(v.File, path) {
			delete(p.Elements, k)
		}
	}
	for k, v := range p.Questions {
		if samePath(v.File, path) {
			delete(p.Questions, k)
		}
	}
	if e != nil {
		p.Elements[e.ID] = e
		p.Reorder()
		return e.ID, nil
	}
	p.Questions[q.ID] = q
	return q.ID, nil
}

// QuestionsDir などのパス。
func (p *Project) CanonDir() string     { return filepath.Join(p.Root, Dir, "canon") }
func (p *Project) QuestionsDir() string { return filepath.Join(p.Root, Dir, "questions") }
func (p *Project) SourcesDir() string   { return filepath.Join(p.Root, Dir, "sources") }

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}
