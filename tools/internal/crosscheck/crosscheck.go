// Package crosscheck は、要素が出典にしている設計書の記述とコードを機械的に突き合わせる。
//
// 各要素について、その項目が出典にしている情報源の箇所（アンカーの本文）から
// バッククォートで囲まれた識別子を抜き出し、同じ要素が出典にしているコードの範囲に
// 現れるかを調べる。現れないものは「設計書にはあるがコードに見当たらない記述」として
// 人の確認待ちにする。AI はこれを一致・不一致と判断しない。
package crosscheck

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
	"go.yaml.in/yaml/v3"
)

// Item は照合の1件。
type Item struct {
	Element    string `yaml:"element" json:"element"`
	Anchor     string `yaml:"anchor" json:"anchor"`
	Term       string `yaml:"term" json:"term"`
	Resolution string `yaml:"resolution" json:"resolution"` // open / ok / conflict / out_of_scope
	By         string `yaml:"by,omitempty" json:"by,omitempty"`
	At         string `yaml:"at,omitempty" json:"at,omitempty"`
	Note       string `yaml:"note,omitempty" json:"note,omitempty"`
}

// File は .canon/crosscheck.yaml。
type File struct {
	Items []Item `yaml:"items"`
}

// Report は照合の結果。
type Report struct {
	Elements int    `json:"elements"` // 照合できた要素（設計書とコードの両方を出典に持つ）
	Skipped  int    `json:"skipped"`  // 片方しか出典に無く照合できなかった要素
	Terms    int    `json:"terms"`    // 調べた識別子
	Matched  int    `json:"matched"`  // コードに見つかった識別子
	Outside  int    `json:"outside"`  // 対象のコードのどこにも無い識別子（別の範囲の話として照合しない）
	Items    []Item `json:"items"`    // 見つからなかった識別子（人の確認の状態付き）
	Open     int    `json:"open"`
}

var anchorRe = regexp.MustCompile(`<!--\s*src:\s*(.+?)\s*-->`)
var tickRe = regexp.MustCompile("`([^`\n]+)`")
var codeRefRe = regexp.MustCompile(`^(.+?):(\d+)(?:-(\d+))?$`)

// Path は照合結果の保存先。
func Path(p *project.Project) string { return filepath.Join(p.Root, project.Dir, "crosscheck.yaml") }

// Run は照合を行い、既存の人の判断を引き継いで .canon/crosscheck.yaml を書き直す。
func Run(p *project.Project, write bool) (*Report, error) {
	blocks := anchorBlocks(p.SourcesDir())
	prev := map[string]Item{}
	if data, err := os.ReadFile(Path(p)); err == nil {
		var f File
		if yaml.Unmarshal(data, &f) == nil {
			for _, it := range f.Items {
				prev[key(it)] = it
			}
		}
	}
	r := &Report{}
	seen := map[string]bool{}
	scope := scopeText(p)
	for _, id := range p.Order {
		e := p.Elements[id]
		docs, codes := refs(e)
		if len(docs) == 0 || len(codes) == 0 {
			r.Skipped++
			continue
		}
		r.Elements++
		code := codeText(p.Root, codes)
		accounted := accountedText(p, e)
		for _, a := range docs {
			text, ok := blocks[model.NormalizeRef(a)]
			if !ok {
				continue
			}
			for _, term := range terms(text) {
				k := e.ID + "\x00" + a + "\x00" + term
				if seen[k] {
					continue
				}
				seen[k] = true
				r.Terms++
				if found(term, code) {
					r.Matched++
					continue
				}
				if !found(term, scope) {
					r.Outside++ // 対象のコードのどこにも無い。別の範囲の話
					continue
				}
				if found(term, accounted) {
					r.Matched++ // 矛盾・質問として既に人に回っている
					continue
				}
				it := Item{Element: e.ID, Anchor: a, Term: term, Resolution: "open"}
				if old, ok := prev[key(it)]; ok {
					it = old
				}
				if it.Resolution == "open" {
					r.Open++
				}
				r.Items = append(r.Items, it)
			}
		}
	}
	sort.SliceStable(r.Items, func(i, j int) bool {
		a, b := r.Items[i], r.Items[j]
		if a.Element != b.Element {
			return a.Element < b.Element
		}
		return a.Term < b.Term
	})
	if write {
		data, _ := yaml.Marshal(File{Items: r.Items})
		head := "# canon crosscheck が生成。設計書の記述（識別子）のうち、同じ要素が出典にしているコードに見当たらないもの。\n" +
			"# resolution は人が決める：ok（問題なし）／conflict（矛盾。Canon で conflict にして質問を起票）／out_of_scope（この範囲のコードの話ではない）\n" +
			"# AI は resolution を open 以外にしない。\n"
		if err := os.WriteFile(Path(p), append([]byte(head), data...), 0o644); err != nil {
			return r, err
		}
	}
	return r, nil
}

// scopeText は Canon が出典にしているコードのファイル全体（対象のコード）。
func scopeText(p *project.Project) string {
	files := map[string]bool{}
	for _, e := range p.Elements {
		_, codes := refs(e)
		for _, c := range codes {
			if m := codeRefRe.FindStringSubmatch(c); m != nil {
				files[m[1]] = true
			}
		}
	}
	var b strings.Builder
	for f := range files {
		if data, err := os.ReadFile(filepath.Join(p.Root, filepath.FromSlash(f))); err == nil {
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func key(it Item) string { return it.Element + "\x00" + it.Anchor + "\x00" + it.Term }

// refs は要素の項目（リンクは除く）が出典にしている文書のアンカーとコードの範囲。
func refs(e *model.Element) (docs, codes []string) {
	ds, cs := map[string]bool{}, map[string]bool{}
	add := func(s *model.Source) {
		if s == nil {
			return
		}
		switch s.Kind {
		case "document":
			ds[s.Ref] = true
		case "code":
			cs[s.Ref] = true
		}
	}
	for _, f := range e.Fields {
		add(f.Source)
		for _, s := range f.Also {
			add(s)
		}
		for _, c := range f.Claims {
			add(c.Source)
		}
	}
	for k := range ds {
		docs = append(docs, k)
	}
	for k := range cs {
		codes = append(codes, k)
	}
	sort.Strings(docs)
	sort.Strings(codes)
	return
}

// accountedText は、矛盾の主張・質問の文脈として既に人に回っている文字列。
func accountedText(p *project.Project, e *model.Element) string {
	var b strings.Builder
	for _, f := range e.Fields {
		for _, c := range f.Claims {
			b.WriteString(model.Normalize(c.Value) + "\n")
		}
		if q, ok := p.Questions[f.Question]; ok {
			b.WriteString(q.Context + "\n")
			for _, o := range q.Options {
				b.WriteString(fmt.Sprint(o) + "\n")
			}
		}
	}
	return b.String()
}

func anchorBlocks(dir string) map[string]string {
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
		cur := ""
		var b strings.Builder
		flush := func() {
			if cur != "" {
				out[cur] = b.String()
			}
			b.Reset()
		}
		for sc.Scan() {
			line := sc.Text()
			if m := anchorRe.FindStringSubmatch(line); m != nil {
				flush()
				cur = model.NormalizeRef(strings.ReplaceAll(m[1], "|", " "))
				continue
			}
			b.WriteString(line + "\n")
		}
		flush()
		return nil
	})
	return out
}

func codeText(root string, refs []string) string {
	var b strings.Builder
	cache := map[string][]string{}
	for _, r := range refs {
		m := codeRefRe.FindStringSubmatch(r)
		if m == nil {
			continue
		}
		lines, ok := cache[m[1]]
		if !ok {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m[1])))
			if err != nil {
				continue
			}
			lines = strings.Split(string(data), "\n")
			cache[m[1]] = lines
		}
		from, _ := strconv.Atoi(m[2])
		to := from
		if m[3] != "" {
			to, _ = strconv.Atoi(m[3])
		}
		for i := from; i <= to && i <= len(lines); i++ {
			if i >= 1 {
				b.WriteString(lines[i-1] + "\n")
			}
		}
	}
	return b.String()
}

// terms は記述からバッククォートの識別子を抜き出す。短すぎるものは除く。
func terms(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range tickRe.FindAllStringSubmatch(text, -1) {
		t := strings.TrimSpace(m[1])
		if len([]rune(t)) < 3 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

var splitRe = regexp.MustCompile(`::|›|/|\s+|\(|\)|,|=`)

// found は識別子 term がコード text に現れるか。全体か、意味のある最後の区切りが現れれば見つかったとする。
func found(term, text string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, term) {
		return true
	}
	t := strings.TrimPrefix(term, ".")
	if len(t) >= 3 && strings.Contains(text, t) {
		return true
	}
	parts := splitRe.Split(term, -1)
	for i := len(parts) - 1; i >= 0; i-- {
		s := strings.Trim(parts[i], ".:'\"<>")
		if len(s) >= 4 {
			return strings.Contains(text, s)
		}
	}
	return false
}

// Text は人が読む形。
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "照合した要素: %d（設計書とコードの片方しか出典に無く照合できない要素: %d）\n", r.Elements, r.Skipped)
	fmt.Fprintf(&b, "調べた識別子: %d（出典のコードに有る %d／対象のコードのどこにも無い＝別の範囲の話 %d）\n", r.Terms, r.Matched, r.Outside)
	fmt.Fprintf(&b, "食い違いの候補（対象のコードには有るが、その要素の出典のコードに無い）: %d（人の確認待ち %d）\n", len(r.Items), r.Open)
	cur := ""
	for _, it := range r.Items {
		if it.Element != cur {
			cur = it.Element
			fmt.Fprintf(&b, "\n%s\n", cur)
		}
		fmt.Fprintf(&b, "  [%s] `%s`（%s）\n", it.Resolution, it.Term, it.Anchor)
	}
	return b.String()
}
