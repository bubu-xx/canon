package gaps

import (
	"bufio"
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

// コードの網羅：対象範囲のすべての関数が、Canon のいずれかの要素の出典（コードの位置）に含まれているか。
// 要素の粒度をどう決めても、Canon から指されないコードが無いことを確かめる。
// 関数の範囲は「その関数の定義行から、同じファイルの次の関数の定義行の手前まで」とみなし、
// 出典のコードの位置（パス:行 または パス:開始-終了）がその範囲に重なれば含まれているとする。

// UncoveredFile は、出典に含まれていない関数のあるファイル。
type UncoveredFile struct {
	File      string   `json:"file"`
	Functions []string `json:"functions"` // 「名前:行」
	Total     int      `json:"total"`     // ファイルの関数の数
}

// IgnoredCode は人が「対象外」と判断したコード（.canon/coverage.yaml の ignored_code）。
type IgnoredCode struct {
	Path   string `yaml:"path"` // パスの先頭、または glob（例: video/generated/**、*.pb.go）
	Reason string `yaml:"reason"`
	By     string `yaml:"by"`
}

var funcRes = map[string][]*regexp.Regexp{
	".rs": {regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:(?:const|async|unsafe|default)\s+|extern\s+"[^"]*"\s+)*fn\s+([A-Za-z_][A-Za-z0-9_]*)`)},
	".go": {regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)},
	".py": {regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)`)},
	".ts": {
		regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`),
		regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let)\s+([A-Za-z_$][\w$]*)\s*(?::[^=]+)?=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*(?::\s*[^=]+)?=>`),
	},
}

func init() {
	for _, e := range []string{".tsx", ".js", ".jsx", ".mjs"} {
		funcRes[e] = funcRes[".ts"]
	}
}

var skipCodeDir = map[string]bool{".git": true, ".canon": true, "target": true, "node_modules": true, "vendor": true, "dist": true, "build": true}

var codeRefRe = regexp.MustCompile(`^(.+?):(\d+)(?:-(\d+))?$`)

type span struct{ a, b int }

// CodeCoverage は関数の網羅を数える。
func CodeCoverage(p *project.Project) (covered, total int, files []UncoveredFile) {
	refs := map[string][]span{}
	mark := func(s *model.Source) {
		if s == nil || s.Kind != "code" {
			return
		}
		m := codeRefRe.FindStringSubmatch(strings.TrimSpace(s.Ref))
		if m == nil {
			return
		}
		a, _ := strconv.Atoi(m[2])
		b := a
		if m[3] != "" {
			b, _ = strconv.Atoi(m[3])
		}
		path := filepath.ToSlash(m[1])
		refs[path] = append(refs[path], span{a, b})
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
	ignored := loadIgnoredCode(p)
	roots := []string{"."}
	if len(p.Config.Scope) > 0 {
		roots = nil
		for _, s := range p.Config.Scope {
			if st, err := os.Stat(filepath.Join(p.Root, s)); err == nil && st.IsDir() {
				roots = append(roots, s)
			}
		}
		if len(roots) == 0 {
			roots = []string{"."}
		}
	}
	for _, r := range roots {
		_ = filepath.WalkDir(filepath.Join(p.Root, r), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipCodeDir[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			res := funcRes[strings.ToLower(filepath.Ext(path))]
			if res == nil {
				return nil
			}
			rel, _ := filepath.Rel(p.Root, path)
			rel = filepath.ToSlash(rel)
			if isIgnoredCode(rel, ignored) {
				return nil
			}
			fns := functions(path, res)
			if len(fns) == 0 {
				return nil
			}
			uf := UncoveredFile{File: rel, Total: len(fns)}
			for i, fn := range fns {
				end := 1 << 30
				if i+1 < len(fns) {
					end = fns[i+1].line - 1
				}
				total++
				hit := false
				for _, s := range refs[rel] {
					if s.a <= end && s.b >= fn.line {
						hit = true
						break
					}
				}
				if hit {
					covered++
				} else {
					uf.Functions = append(uf.Functions, fn.name+":"+strconv.Itoa(fn.line))
				}
			}
			if len(uf.Functions) > 0 {
				files = append(files, uf)
			}
			return nil
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
	return
}

type fnDef struct {
	name string
	line int
}

func functions(path string, res []*regexp.Regexp) []fnDef {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []fnDef
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		for _, re := range res {
			if m := re.FindStringSubmatch(line); m != nil {
				out = append(out, fnDef{m[1], n})
				break
			}
		}
	}
	return out
}

func loadIgnoredCode(p *project.Project) []IgnoredCode {
	var ig struct {
		IgnoredCode []IgnoredCode `yaml:"ignored_code"`
	}
	if data, err := os.ReadFile(filepath.Join(p.Root, project.Dir, "coverage.yaml")); err == nil {
		_ = yaml.Unmarshal(data, &ig)
	}
	var out []IgnoredCode
	for _, x := range ig.IgnoredCode {
		if x.By != "" && x.Path != "" { // 誰が判断したかの無いものは効かない
			out = append(out, x)
		}
	}
	return out
}

func isIgnoredCode(rel string, ig []IgnoredCode) bool {
	for _, x := range ig {
		pat := filepath.ToSlash(x.Path)
		if strings.HasSuffix(pat, "/**") {
			if strings.HasPrefix(rel, strings.TrimSuffix(pat, "**")) {
				return true
			}
			continue
		}
		if strings.HasPrefix(rel, strings.TrimSuffix(pat, "/")+"/") || rel == pat {
			return true
		}
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, filepath.Base(rel)); ok && !strings.Contains(pat, "/") {
			return true
		}
	}
	return false
}
