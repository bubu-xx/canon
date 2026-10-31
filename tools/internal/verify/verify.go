// Package verify は「箇所単位の照合」（.canon/verify.yaml）を検査する。
//
// 情報源のすべてのアンカーについて、記述を文の単位に分け、それぞれを実現しているコードの位置と
// 判定（一致・不一致・未実装・照合不能）を記録する。この検査は記録の形と網羅を確かめる：
//   - すべてのアンカーに照合の記録がある
//   - 判定が決められた値のどれか
//   - コードの位置が実在する
//   - 不一致・未実装には質問が付いている
//   - 範囲外・照合不能には理由がある
package verify

import (
	"bufio"
	"fmt"
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

// Statement は1つの文の照合結果。
type Statement struct {
	Text     string   `yaml:"text" json:"text"`
	Scope    string   `yaml:"scope" json:"scope"`     // in / out
	Code     []string `yaml:"code" json:"code"`       // 実現しているコード（パス:行）
	Verdict  string   `yaml:"verdict" json:"verdict"` // consistent / inconsistent / not_implemented / not_checkable
	Note     string   `yaml:"note" json:"note"`
	Question string   `yaml:"question" json:"question"`
	Review   string   `yaml:"review" json:"review"` // 一致＋注記のとき、別のエージェントの見直し：kept / changed
}

// Entry は1つのアンカーの照合結果。
type Entry struct {
	Anchor     string      `yaml:"anchor" json:"anchor"`
	Statements []Statement `yaml:"statements" json:"statements"`
}

// File は .canon/verify.yaml。
type File struct {
	Entries []Entry `yaml:"entries"`
}

// Issue は検査で見つかった問題。
type Issue struct {
	Anchor  string `json:"anchor"`
	Message string `json:"message"`
}

// Report は検査の結果。
type Report struct {
	Anchors    int            `json:"anchors"`
	Verified   int            `json:"verified"`
	Statements int            `json:"statements"`
	Verdicts   map[string]int `json:"verdicts"`
	OutOfScope int            `json:"out_of_scope"`
	NeedReview int            `json:"need_review"`
	Reviewed   int            `json:"reviewed"`
	Findings   []Finding      `json:"findings"` // 不一致・未実装（人が判断する）
	Gaps       []Finding      `json:"gaps"`     // 記述不足（コードと矛盾はしないが、記述に無い条件・例外がある）
	Issues     []Issue        `json:"issues"`
}

// Finding は人に回す食い違い。
type Finding struct {
	Anchor   string `json:"anchor"`
	Text     string `json:"text"`
	Verdict  string `json:"verdict"`
	Note     string `json:"note"`
	Question string `json:"question"`
}

var codeRefRe = regexp.MustCompile(`^(.+?):(\d+)(?:-(\d+))?$`)

var verdicts = map[string]bool{"consistent": true, "inconsistent": true, "incomplete": true, "not_implemented": true, "not_checkable": true}

// Path は保存先。
func Path(p *project.Project) string { return filepath.Join(p.Root, project.Dir, "verify.yaml") }

// Check は .canon/verify.yaml を検査する。
func Check(p *project.Project) (*Report, error) {
	r := &Report{Verdicts: map[string]int{}}
	var f File
	data, err := os.ReadFile(Path(p))
	if err == nil {
		if err := yaml.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("verify.yaml が読めません: %w", err)
		}
	}
	byAnchor := map[string]*Entry{}
	for i := range f.Entries {
		e := &f.Entries[i]
		k := model.NormalizeRef(e.Anchor)
		if _, dup := byAnchor[k]; dup {
			r.Issues = append(r.Issues, Issue{e.Anchor, "同じ箇所の記録が重複しています"})
		}
		byAnchor[k] = e
		if _, ok := p.Anchors[k]; !ok {
			r.Issues = append(r.Issues, Issue{e.Anchor, "情報源にこの箇所（アンカー）がありません"})
		}
	}
	var anchors []string
	for a := range p.Anchors {
		anchors = append(anchors, a)
	}
	sort.Strings(anchors)
	r.Anchors = len(anchors)
	lines := map[string]int{}
	for _, a := range anchors {
		e, ok := byAnchor[a]
		if !ok {
			r.Issues = append(r.Issues, Issue{a, "照合されていません"})
			continue
		}
		if len(e.Statements) == 0 {
			r.Issues = append(r.Issues, Issue{a, "文の照合が1つもありません"})
			continue
		}
		r.Verified++
		for _, s := range e.Statements {
			r.Statements++
			where := a + "「" + short(s.Text) + "」"
			if strings.TrimSpace(s.Text) == "" {
				r.Issues = append(r.Issues, Issue{a, "text（照合した文）がありません"})
			}
			switch s.Scope {
			case "out":
				r.OutOfScope++
				if strings.TrimSpace(s.Note) == "" {
					r.Issues = append(r.Issues, Issue{where, "範囲外（scope: out）には、何の話なのかを note に書いてください"})
				}
				continue
			case "in":
			default:
				r.Issues = append(r.Issues, Issue{where, "scope は in / out のどちらかにしてください"})
				continue
			}
			if !verdicts[s.Verdict] {
				r.Issues = append(r.Issues, Issue{where, fmt.Sprintf("verdict %q は使えません（consistent / inconsistent / not_implemented / not_checkable）", s.Verdict)})
				continue
			}
			r.Verdicts[s.Verdict]++
			for _, c := range s.Code {
				if msg := checkCode(p.Root, c, lines); msg != "" {
					r.Issues = append(r.Issues, Issue{where, msg})
				}
			}
			if strings.TrimSpace(s.Note) != "" && (s.Verdict == "consistent" || s.Review == "changed") {
				r.NeedReview++
				switch s.Review {
				case "kept", "changed":
					r.Reviewed++
				case "":
					r.Issues = append(r.Issues, Issue{where, "一致なのに注記があります。差に気づいている可能性があるので、別のエージェントの見直し（review: kept / changed）が必要です"})
				default:
					r.Issues = append(r.Issues, Issue{where, "review は kept / changed のどちらかにしてください"})
				}
			}
			switch s.Verdict {
			case "consistent", "inconsistent", "incomplete":
				if len(s.Code) == 0 {
					r.Issues = append(r.Issues, Issue{where, s.Verdict + " にはコードの位置（code）が必要です"})
				}
			}
			switch s.Verdict {
			case "inconsistent", "not_implemented":
				r.Findings = append(r.Findings, Finding{a, s.Text, s.Verdict, s.Note, s.Question})
				if s.Question == "" {
					r.Issues = append(r.Issues, Issue{where, s.Verdict + " には質問（question）が必要です。どちらが正しいかは人が決める"})
				} else if _, ok := p.Questions[s.Question]; !ok {
					r.Issues = append(r.Issues, Issue{where, "質問 " + s.Question + " が見つかりません"})
				}
				if strings.TrimSpace(s.Note) == "" {
					r.Issues = append(r.Issues, Issue{where, "何が違うのかを note に書いてください"})
				}
			case "incomplete":
				r.Gaps = append(r.Gaps, Finding{a, s.Text, s.Verdict, s.Note, s.Question})
				if strings.TrimSpace(s.Note) == "" {
					r.Issues = append(r.Issues, Issue{where, "記述に無い条件・例外・挙動を note に書いてください"})
				}
			case "not_checkable":
				if strings.TrimSpace(s.Note) == "" {
					r.Issues = append(r.Issues, Issue{where, "照合できない理由を note に書いてください"})
				}
			}
		}
	}
	return r, nil
}

func checkCode(root, ref string, cache map[string]int) string {
	m := codeRefRe.FindStringSubmatch(ref)
	if m == nil {
		return fmt.Sprintf("コードの位置 %q は 'パス:行' の形ではありません", ref)
	}
	n, ok := cache[m[1]]
	if !ok {
		fh, err := os.Open(filepath.Join(root, filepath.FromSlash(m[1])))
		if err != nil {
			return "コード " + m[1] + " が見つかりません"
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
		for sc.Scan() {
			n++
		}
		fh.Close()
		cache[m[1]] = n
	}
	end, _ := strconv.Atoi(m[2])
	if m[3] != "" {
		end, _ = strconv.Atoi(m[3])
	}
	if end > n {
		return fmt.Sprintf("%s は %d 行しかありません", m[1], n)
	}
	return ""
}

func short(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return string(r)
}

// Text は人が読む形。
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "照合済みの箇所: %d / %d、照合した文: %d（範囲外 %d）\n", r.Verified, r.Anchors, r.Statements, r.OutOfScope)
	fmt.Fprintf(&b, "判定: 一致 %d・矛盾 %d・記述不足 %d・未実装 %d・照合不能 %d\n", r.Verdicts["consistent"], r.Verdicts["inconsistent"], r.Verdicts["incomplete"], r.Verdicts["not_implemented"], r.Verdicts["not_checkable"])
	fmt.Fprintf(&b, "見直し: 一致＋注記 %d 件のうち見直し済み %d 件\n", r.NeedReview, r.Reviewed)
	if len(r.Findings) > 0 {
		b.WriteString("\n■ 矛盾・未実装（人の判断が要る。どちらが正しいかで、実装の修正か設計書の修正かが決まる）\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "  [%s] %s「%s」→ %s\n      %s\n", f.Verdict, f.Anchor, short(f.Text), or(f.Question, "質問なし"), f.Note)
		}
	}
	if len(r.Gaps) > 0 {
		b.WriteString("\n■ 記述不足（矛盾ではないが、設計書に無い条件・例外がコードにある。設計書の追記候補）\n")
		for _, f := range r.Gaps {
			fmt.Fprintf(&b, "  %s「%s」\n      %s\n", f.Anchor, short(f.Text), f.Note)
		}
	}
	if len(r.Issues) > 0 {
		fmt.Fprintf(&b, "\n■ 記録の不備 %d 件\n", len(r.Issues))
		for _, i := range r.Issues {
			fmt.Fprintf(&b, "  %s: %s\n", i.Anchor, i.Message)
		}
	}
	return b.String()
}

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
