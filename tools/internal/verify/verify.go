// Package verify は「箇所単位の照合」（.canon/verify.yaml）を検査する。
//
// 情報源のすべてのアンカーを、道具が機械的に作った区切り（unit）に分け、区切りごとに
// 実現しているコードの位置と判定（一致・矛盾・記述不足・未実装・照合不能）を記録する。
// この検査は記録の形と網羅を確かめる：
//   - すべてのアンカーのすべての区切りに照合の記録がある（区切りの文面は書き換えない）
//   - 判定が決められた値のどれか
//   - コードの位置が実在する
//   - 矛盾・未実装には質問が付いている
//   - 範囲外・照合不能には理由がある
//   - 一致・記述不足には、照合していない別のエージェントの反例探し（review）が済んでいる
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
	Unit       int      `yaml:"unit" json:"unit"`     // 区切りの番号（canon verify --init が付ける。1 から）
	Source     string   `yaml:"source" json:"source"` // 区切りの文面（書き換えない）
	Text       string   `yaml:"text" json:"text"`
	Scope      string   `yaml:"scope" json:"scope"`     // in / out
	Code       []string `yaml:"code" json:"code"`       // 実現しているコード（パス:行）
	Verdict    string   `yaml:"verdict" json:"verdict"` // consistent / inconsistent / not_implemented / not_checkable
	Note       string   `yaml:"note" json:"note"`
	Question   string   `yaml:"question" json:"question"`
	Review     string   `yaml:"review" json:"review"`           // 一致・記述不足に対する別のエージェントの反例探し：kept / changed
	ReviewNote string   `yaml:"review_note" json:"review_note"` // changed のとき、見つけた反例（入力・状態とコードの位置）
	// Affects は、この文の矛盾・未実装が Canon のどの項目の話か（"BHV-003.error_cases" の形）。
	// 矛盾・未実装には必須。その項目は Canon で conflict（未実装は unknown も可）になり、同じ質問につながる
	Affects []string `yaml:"affects" json:"affects"`
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
	Units      int            `json:"units"`
	NeedReview int            `json:"need_review"`
	Reviewed   int            `json:"reviewed"`
	Changed    int            `json:"changed"`
	Findings   []Finding      `json:"findings"` // 不一致・未実装（人が判断する）
	Linked     int            `json:"linked"`   // 矛盾・未実装のうち Canon に反映済み
	Stale      []Stale        `json:"stale"`    // 照合で矛盾が出た箇所を出典にしている確定値（見直し候補）
	Gaps       []Finding      `json:"gaps"`     // 記述不足（コードと矛盾はしないが、記述に無い条件・例外がある）
	Issues     []Issue        `json:"issues"`
}

// Stale は、照合で矛盾・未実装が出た箇所を出典にしているのに、どの文の affects にも挙がっていない Canon の確定値。
type Stale struct {
	Anchor string `json:"anchor"`
	Field  string `json:"field"`
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
	f, err := LoadProject(p)
	if err != nil {
		return nil, err
	}
	units := Units(p.SourcesDir())
	affected := map[string]bool{}   // affects に挙がっている項目（ID.項目）
	badAnchors := map[string]bool{} // 矛盾・未実装が出たアンカー
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
		us := units[a]
		r.Units += len(us)
		covered := map[int]bool{}
		for _, s := range e.Statements {
			if s.Unit < 1 || s.Unit > len(us) {
				r.Issues = append(r.Issues, Issue{a, fmt.Sprintf("unit %d はこの箇所にありません（1〜%d）。canon verify --init で作った区切りを使ってください", s.Unit, len(us))})
			} else {
				covered[s.Unit] = true
				if model.NormalizeRef(s.Source) != model.NormalizeRef(us[s.Unit-1]) {
					r.Issues = append(r.Issues, Issue{fmt.Sprintf("%s #%d", a, s.Unit), "source（区切りの文面）が情報源と違います。書き換えずに、分けたいときは text で分けてください"})
				}
			}
		}
		for i := range us {
			if !covered[i+1] {
				r.Issues = append(r.Issues, Issue{fmt.Sprintf("%s #%d", a, i+1), "この区切りが照合されていません「" + short(us[i]) + "」"})
			}
		}
		for _, s := range e.Statements {
			r.Statements++
			where := fmt.Sprintf("%s #%d「%s」", a, s.Unit, short(s.Text))
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
			if s.Verdict == "consistent" || s.Verdict == "incomplete" || s.Review == "changed" {
				r.NeedReview++
				switch s.Review {
				case "kept":
					r.Reviewed++
				case "changed":
					r.Reviewed++
					r.Changed++
					if strings.TrimSpace(s.ReviewNote) == "" {
						r.Issues = append(r.Issues, Issue{where, "review: changed には、見つけた反例（入力・状態とコードの位置）を review_note に書いてください"})
					}
					if s.Verdict == "consistent" {
						r.Issues = append(r.Issues, Issue{where, "review: changed なのに一致のままです。反例があるなら inconsistent か incomplete にしてください"})
					}
				case "":
					r.Issues = append(r.Issues, Issue{where, s.Verdict + " には、照合していない別のエージェントの反例探し（review: kept / changed）が必要です"})
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
				if msgs := checkAffects(p, s); len(msgs) > 0 {
					for _, m := range msgs {
						r.Issues = append(r.Issues, Issue{where, m})
					}
				} else {
					r.Linked++
				}
				for _, ref := range s.Affects {
					affected[ref] = true
				}
				badAnchors[a] = true
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
	r.Stale = stale(p, badAnchors, affected)
	return r, nil
}

// checkAffects は矛盾・未実装の文が Canon に反映されているかを確かめる。
func checkAffects(p *project.Project, s Statement) []string {
	if len(s.Affects) == 0 {
		return []string{"affects（この食い違いが Canon のどの項目の話か、'BHV-003.error_cases' の形）が必要です。当てはまる要素が無ければ、要素を作ってから挙げてください（情報源の内容は Canon のどこかに入る）"}
	}
	var out []string
	for _, ref := range s.Affects {
		id, field, ok := strings.Cut(ref, ".")
		e := p.Elements[id]
		if !ok || e == nil {
			out = append(out, "affects の "+ref+" の要素が Canon にありません")
			continue
		}
		fl := e.Fields[field]
		if fl == nil {
			out = append(out, "affects の "+ref+" の項目が要素にありません（必要なら unknown か conflict として置く）")
			continue
		}
		switch {
		case fl.Status == model.StatusConfirmed && fl.Source != nil && fl.Source.Kind == "answer":
			// 人が回答して確定した。照合の質問への回答であること
			if s.Question != "" && fl.Source.Question != s.Question {
				out = append(out, fmt.Sprintf("%s は回答（%s）で確定していますが、この食い違いの質問は %s です", ref, fl.Source.Question, s.Question))
			}
		case fl.Status == model.StatusConflict, fl.Status == model.StatusUnknown && s.Verdict == "not_implemented":
			if s.Question != "" && fl.Question != s.Question {
				out = append(out, fmt.Sprintf("%s の質問（%s）と、この食い違いの質問（%s）が違います。同じ判断なら同じ質問にしてください", ref, or(fl.Question, "なし"), s.Question))
			}
		default:
			want := "conflict"
			if s.Verdict == "not_implemented" {
				want = "conflict か unknown"
			}
			out = append(out, fmt.Sprintf("%s は %s ですが、照合で食い違いが出ています。%s にして記述とコードの主張を claims に残してください（人が回答したら answer を出典に確定する）", ref, or(fl.Status, "状態なし"), want))
		}
	}
	return out
}

// stale は、矛盾・未実装の出たアンカーを出典（source / also）にしている確定値のうち、affects に挙がっていないもの。
func stale(p *project.Project, bad, affected map[string]bool) []Stale {
	var out []Stale
	for _, id := range p.Order {
		e := p.Elements[id]
		for _, name := range e.FieldOrder {
			fl := e.Fields[name]
			if fl == nil || fl.Status != model.StatusConfirmed {
				continue
			}
			srcs := append([]*model.Source{fl.Source}, fl.Also...)
			for _, src := range srcs {
				if src == nil || src.Kind != "document" {
					continue
				}
				a := model.NormalizeRef(src.Ref)
				if bad[a] && !affected[id+"."+name] {
					out = append(out, Stale{a, id + "." + name})
				}
			}
		}
	}
	return out
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

// Dir は分けて記録するときの置き場（.canon/verify/*.yaml）。
func Dir(p *project.Project) string { return filepath.Join(p.Root, project.Dir, "verify") }

// LoadProject はプロジェクトの照合の記録をすべて読む（.canon/verify.yaml と .canon/verify/*.yaml）。
func LoadProject(p *project.Project) (*File, error) {
	f, err := Load(Path(p))
	if err != nil {
		return nil, err
	}
	d, err := Load(Dir(p))
	if err != nil {
		return nil, err
	}
	f.Entries = append(f.Entries, d.Entries...)
	return f, nil
}

// Load は照合の記録を読む（無ければ空）。path がディレクトリなら、その中の *.yaml をすべて読む。
func Load(path string) (*File, error) {
	var f File
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		ents, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			g, err := Load(filepath.Join(path, e.Name()))
			if err != nil {
				return nil, err
			}
			f.Entries = append(f.Entries, g.Entries...)
		}
		return &f, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &f, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s が読めません: %w", path, err)
	}
	return &f, nil
}

// Text は人が読む形。
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "照合済みの箇所: %d / %d（区切り %d）、照合した文: %d（範囲外 %d）\n", r.Verified, r.Anchors, r.Units, r.Statements, r.OutOfScope)
	fmt.Fprintf(&b, "判定: 一致 %d・矛盾 %d・記述不足 %d・未実装 %d・照合不能 %d\n", r.Verdicts["consistent"], r.Verdicts["inconsistent"], r.Verdicts["incomplete"], r.Verdicts["not_implemented"], r.Verdicts["not_checkable"])
	fmt.Fprintf(&b, "反例探し: 一致・記述不足 %d 件のうち済み %d 件（判定が変わったもの %d 件）\n", r.NeedReview, r.Reviewed, r.Changed)
	if len(r.Findings) > 0 {
		b.WriteString("\n■ 矛盾・未実装（人の判断が要る。どちらが正しいかで、実装の修正か設計書の修正かが決まる）\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "  [%s] %s「%s」→ %s\n      %s\n", f.Verdict, f.Anchor, short(f.Text), or(f.Question, "質問なし"), f.Note)
		}
	}
	if n := len(r.Findings); n > 0 {
		fmt.Fprintf(&b, "\nCanon への反映: 矛盾・未実装 %d 件のうち %d 件\n", n, r.Linked)
	}
	if len(r.Stale) > 0 {
		b.WriteString("\n■ 見直し候補（照合で食い違いが出た箇所を出典にしている確定値。食い違いと関係があれば affects に挙げて conflict にする。関係なければそのまま）\n")
		for _, st := range r.Stale {
			fmt.Fprintf(&b, "  %s ← %s\n", st.Field, st.Anchor)
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
