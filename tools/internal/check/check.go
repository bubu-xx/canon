// Package check は Canon が規則どおりかを検査する（validate）。
package check

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
	"github.com/bubu-xx/canon/tools/internal/schema"
)

// Issue は検査で見つかった問題。
type Issue struct {
	Severity string `json:"severity"` // error / warning
	Code     string `json:"code"`
	File     string `json:"file,omitempty"`
	Element  string `json:"element,omitempty"`
	Field    string `json:"field,omitempty"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

func (i Issue) String() string {
	where := i.Element
	if i.Field != "" {
		where += "." + i.Field
	}
	if where == "" && i.File != "" {
		where = filepath.Base(i.File)
	}
	if where == "" {
		where = "全体"
	}
	s := fmt.Sprintf("[%s] %s: %s", label(i.Severity), where, i.Message)
	if i.Hint != "" {
		s += "\n    → " + i.Hint
	}
	return s
}

func label(sev string) string {
	if sev == "error" {
		return "エラー"
	}
	return "警告"
}

// Options は検査の範囲。
type Options struct {
	Only    map[string]bool // 空なら全体。要素 ID・質問 ID で絞る
	Lenient bool            // 書き込み途中の検査。まだ作っていない相手への参照は警告にとどめる
}

type checker struct {
	p             *project.Project
	opt           Options
	issues        []Issue
	warnedAnchors bool
}

// Run は検査を実行する。
func Run(p *project.Project, opt Options) []Issue {
	c := &checker{p: p, opt: opt}
	for _, fe := range p.LoadErrors {
		if len(opt.Only) == 0 {
			c.add("error", "unreadable", fe.File, "", "", fe.Err.Error(), "YAML の書式を直してください")
		}
	}
	for _, id := range p.Order {
		e := p.Elements[id]
		if c.in(e.ID) {
			c.element(e)
		}
	}
	qids := make([]string, 0, len(p.Questions))
	for id := range p.Questions {
		qids = append(qids, id)
	}
	sort.Strings(qids)
	for _, id := range qids {
		q := p.Questions[id]
		if c.in(q.ID) {
			c.question(q)
		}
	}
	return c.issues
}

// Errors は error の件数。
func Errors(is []Issue) int {
	n := 0
	for _, i := range is {
		if i.Severity == "error" {
			n++
		}
	}
	return n
}

func (c *checker) in(id string) bool {
	return len(c.opt.Only) == 0 || c.opt.Only[id]
}

var lenientCodes = map[string]bool{"dangling_link": true, "dangling_asks_for": true}

func (c *checker) add(sev, code, file, el, field, msg, hint string) {
	if c.opt.Lenient && sev == "error" && lenientCodes[code] {
		sev = "warning"
		hint = strings.TrimSpace(hint + "（作業終了時の検査ではエラーになります）")
	}
	c.issues = append(c.issues, Issue{sev, code, file, el, field, msg, hint})
}

var idRe = regexp.MustCompile(`^[A-Z]+-[0-9]+$`)

var elementKeys = set("id", "type", "name", "owner", "version", "fields", "links")
var fieldKeys = set("status", "value", "unit", "source", "also", "claims", "question")

func (c *checker) element(e *model.Element) {
	f := e.File
	for _, pr := range e.Problems {
		c.add("error", "shape", f, e.ID, "", pr, "")
	}
	for _, k := range e.Keys {
		if !elementKeys[k] {
			c.add("error", "unknown_key", f, e.ID, "", fmt.Sprintf("要素に定義されていないキー %q があります", k), "fields の中に入れるか削除してください")
		}
	}
	if e.ID == "" {
		c.add("error", "missing_id", f, "", "", "id がありません", "型の接頭辞＋連番（例: BHV-001）を付けてください")
	} else if !idRe.MatchString(e.ID) {
		c.add("error", "bad_id", f, e.ID, "", "id の形式が不正です", "接頭辞-数字（例: REQ-001）にしてください")
	}
	if e.Name == "" {
		c.add("error", "missing_name", f, e.ID, "", "name がありません", "人が識別できる短い名前を付けてください")
	}
	td := c.p.Schema.Types[e.Type]
	if td == nil {
		c.add("error", "unknown_type", f, e.ID, "", fmt.Sprintf("型 %q はスキーマにありません", e.Type), "標準スキーマの型を使うか、.canon/schema-ext/types/ に型を追加してください")
		return
	}
	if e.ID != "" && td.Prefix != "" && !strings.HasPrefix(e.ID, td.Prefix+"-") {
		c.add("error", "prefix_mismatch", f, e.ID, "", fmt.Sprintf("型 %s の ID は %s- で始まる必要があります", e.Type, td.Prefix), "")
	}
	if e.ID != "" && !strings.EqualFold(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)), e.ID) {
		c.add("warning", "file_name", f, e.ID, "", "ファイル名が ID と一致していません", e.ID+".yaml にしてください")
	}
	for _, name := range td.FieldOrder {
		def := td.Fields[name]
		if def.Required {
			if _, ok := e.Fields[name]; !ok {
				c.add("error", "missing_field", f, e.ID, name, fmt.Sprintf("必須項目「%s」がありません", def.Label),
					"根拠のある値が無ければ status: unknown として置き、質問を起票してください（空白を隠さない）")
			}
		}
	}
	for _, name := range e.FieldOrder {
		fl := e.Fields[name]
		def := td.Fields[name]
		if def == nil {
			c.add("error", "unknown_field", f, e.ID, name, fmt.Sprintf("項目 %q はスキーマにありません", name),
				"必要な項目なら .canon/schema-ext/ に追加してください（スキーマ改修候補）")
			continue
		}
		c.field(e, fl, def)
	}
	for _, l := range e.Links {
		c.link(e, l)
	}
}

func (c *checker) field(e *model.Element, fl *model.Field, def *schema.FieldDef) {
	f := e.File
	for _, k := range fl.Keys {
		if !fieldKeys[k] {
			c.add("error", "unknown_key", f, e.ID, fl.Name, fmt.Sprintf("項目に定義されていないキー %q があります", k), "使えるキー: status, value, unit, source, claims, question")
		}
	}
	switch fl.Status {
	case model.StatusConfirmed:
		if !fl.HasValue {
			c.add("error", "confirmed_without_value", f, e.ID, fl.Name, "confirmed なのに value がありません", "値が無いなら status: unknown にして質問を起票してください")
		}
		if fl.Source == nil {
			c.add("error", "no_source", f, e.ID, fl.Name, "出典の無い値です。根拠のない値は Canon に入れられません",
				"情報源の位置を source に書くか、根拠が無ければ status: unknown にして質問を起票してください")
		} else {
			c.source(e, fl.Name, fl.Source)
		}
		for _, s := range fl.Also {
			c.source(e, fl.Name, s)
		}
		if len(fl.Claims) > 0 {
			c.add("error", "forbidden_key", f, e.ID, fl.Name, "confirmed に claims は書けません", "")
		}
	case model.StatusUnknown:
		if fl.HasValue {
			c.add("error", "unknown_with_value", f, e.ID, fl.Name, "unknown なのに value があります（推測で埋めない）", "根拠があるなら confirmed＋source、無いなら value を消してください")
		}
		if len(fl.Claims) > 0 {
			c.add("error", "forbidden_key", f, e.ID, fl.Name, "unknown に claims は書けません", "食い違いなら status: conflict にしてください")
		}
		c.questionRef(e, fl)
	case model.StatusConflict:
		if fl.HasValue {
			c.add("error", "conflict_with_value", f, e.ID, fl.Name, "conflict なのに value があります（どちらかを勝手に選ばない）", "value を消し、各主張を claims に残してください")
		}
		if len(fl.Claims) < 2 {
			c.add("error", "few_claims", f, e.ID, fl.Name, "conflict には食い違う主張が2つ以上必要です", "")
		}
		seen := map[string]bool{}
		for i, cl := range fl.Claims {
			if cl.Source == nil {
				c.add("error", "no_source", f, e.ID, fl.Name, fmt.Sprintf("claims[%d] に出典がありません", i), "")
			} else {
				c.source(e, fl.Name, cl.Source)
			}
			if cl.Value != nil {
				c.valueType(e, fl.Name, def, cl.Value)
				n := model.Normalize(cl.Value)
				if seen[n] {
					c.add("warning", "same_claims", f, e.ID, fl.Name, "同じ値の主張が複数あります。矛盾ではない可能性があります", "全情報源で一致しているなら confirmed にしてください")
				}
				seen[n] = true
			}
		}
		c.questionRef(e, fl)
	case model.StatusNA:
		if fl.HasValue || len(fl.Claims) > 0 {
			c.add("error", "na_with_value", f, e.ID, fl.Name, "not_applicable に value・claims は書けません", "")
		}
		if fl.Source == nil {
			c.add("error", "no_source", f, e.ID, fl.Name, "『該当なし』にも根拠が要ります。AI の判断だけでは該当なしにできません",
				"文書にそう書かれていれば document、無ければ質問を起票して人の回答（answer）を根拠にしてください")
		} else if fl.Source.Kind != "document" && fl.Source.Kind != "answer" {
			c.add("error", "na_source_kind", f, e.ID, fl.Name, "『該当なし』の根拠は文書か人の回答に限ります", "")
		} else {
			c.source(e, fl.Name, fl.Source)
		}
	case "":
		c.add("error", "missing_status", f, e.ID, fl.Name, "status がありません", "confirmed / unknown / conflict / not_applicable のいずれかを書いてください")
	default:
		c.add("error", "bad_status", f, e.ID, fl.Name, fmt.Sprintf("status %q は使えません（『たぶん』の状態は作らない）", fl.Status), "confirmed / unknown / conflict / not_applicable のいずれかにしてください")
	}
	if fl.HasValue {
		c.valueType(e, fl.Name, def, fl.Value)
	}
	if fl.Unit != "" && len(def.Units) > 0 && !contains(def.Units, fl.Unit) {
		c.add("error", "bad_unit", f, e.ID, fl.Name, fmt.Sprintf("単位 %q は使えません", fl.Unit), "使える単位: "+strings.Join(def.Units, ", "))
	}
	if fl.HasValue && len(def.Units) > 0 && fl.Unit == "" {
		c.add("error", "missing_unit", f, e.ID, fl.Name, "単位がありません", "使える単位: "+strings.Join(def.Units, ", "))
	}
}

func (c *checker) valueType(e *model.Element, field string, def *schema.FieldDef, v any) {
	ok := true
	switch def.Type {
	case "text", "string":
		_, ok = v.(string)
	case "integer":
		_, ok = v.(int)
	case "number":
		switch v.(type) {
		case int, float64:
		default:
			ok = false
		}
	case "boolean":
		_, ok = v.(bool)
	case "list":
		_, ok = v.([]any)
	case "enum":
		s, isStr := v.(string)
		if !isStr || !contains(def.Values, s) {
			c.add("error", "bad_enum", e.File, e.ID, field, fmt.Sprintf("値 %v は選択肢にありません", v), "選択肢: "+strings.Join(def.Values, ", "))
		}
		return
	}
	if !ok {
		c.add("error", "bad_type", e.File, e.ID, field, fmt.Sprintf("値の型が %s ではありません", def.Type), "")
	}
}

func (c *checker) questionRef(e *model.Element, fl *model.Field) {
	if fl.Question == "" {
		c.add("error", "no_question", e.File, e.ID, fl.Name, "空白・矛盾に対応する質問がありません", "質問を .canon/questions/ に起票し、その ID を question に書いてください")
		return
	}
	q, ok := c.p.Questions[fl.Question]
	if !ok {
		c.add("error", "missing_question", e.File, e.ID, fl.Name, fmt.Sprintf("質問 %s が見つかりません", fl.Question), "先に .canon/questions/"+fl.Question+".yaml を作成してください")
		return
	}
	ref := e.ID + "." + fl.Name
	if !contains(q.AsksFor, ref) {
		c.add("error", "question_mismatch", e.File, e.ID, fl.Name, fmt.Sprintf("質問 %s の asks_for に %s がありません", q.ID, ref), "")
	}
	switch q.Status {
	case "answered":
		c.add("warning", "unapplied_answer", e.File, e.ID, fl.Name, fmt.Sprintf("質問 %s は回答済みですが Canon に反映されていません", q.ID), "回答を source.kind: answer として値に反映してください")
	case "withdrawn":
		c.add("error", "withdrawn_question", e.File, e.ID, fl.Name, fmt.Sprintf("質問 %s は取り下げ済みですが、まだ空白・矛盾が残っています", q.ID), "")
	}
}

var sourceKeys = map[string]map[string]bool{
	"document": set("kind", "ref"),
	"code":     set("kind", "ref", "commit"),
	"answer":   set("kind", "by", "at", "question"),
	"derived":  set("kind", "from", "rule"),
}

func (c *checker) source(e *model.Element, field string, s *model.Source) {
	f := e.File
	allowed, ok := sourceKeys[s.Kind]
	if !ok {
		c.add("error", "bad_source_kind", f, e.ID, field, fmt.Sprintf("出典の種類 %q は使えません", s.Kind), "document / code / answer / derived のいずれかにしてください")
		return
	}
	for _, k := range s.Keys {
		if !allowed[k] {
			c.add("error", "unknown_key", f, e.ID, field, fmt.Sprintf("出典（%s）に使えないキー %q があります", s.Kind, k), "")
		}
	}
	switch s.Kind {
	case "document":
		if s.Ref == "" {
			c.add("error", "no_ref", f, e.ID, field, "出典の位置（ref）がありません", "'文書名 版 節／シート・セル範囲' まで書いてください")
			return
		}
		if len(c.p.Anchors) == 0 {
			if !c.warnedAnchors {
				c.warnedAnchors = true
				c.add("warning", "no_anchors", "", "", "", "情報源のアンカー（<!-- src: ... -->）が1つも無いため、文書の出典が実在するか照合できません", ".canon/sources/ に位置情報付きの情報源を置いてください")
			}
		} else if _, ok := c.p.Anchors[model.NormalizeRef(s.Ref)]; !ok {
			c.add("error", "ref_not_found", f, e.ID, field, fmt.Sprintf("出典 %q が情報源に見つかりません", s.Ref),
				"情報源テキストの <!-- src: ... --> と同じ表記で書いてください。見つからない根拠は使えません")
		}
	case "code":
		c.codeRef(e, field, s.Ref)
	case "answer":
		if s.By == "" || s.At == "" || s.Question == "" {
			c.add("error", "answer_incomplete", f, e.ID, field, "回答の出典には by・at・question が必要です", "")
			return
		}
		q, ok := c.p.Questions[s.Question]
		if !ok {
			c.add("error", "missing_question", f, e.ID, field, fmt.Sprintf("回答元の質問 %s が見つかりません", s.Question), "")
		} else if q.Status != "answered" {
			c.add("error", "not_answered", f, e.ID, field, fmt.Sprintf("質問 %s はまだ回答済み（answered）になっていません", s.Question), "質問ファイルに answer と status: answered を書いてください")
		}
	case "derived":
		if len(s.From) == 0 || s.Rule == "" {
			c.add("error", "derived_incomplete", f, e.ID, field, "計算の出典には from と rule が必要です", "")
		}
	}
}

var codeRefRe = regexp.MustCompile(`^(.+?):(\d+)(?:-(\d+))?$`)

func (c *checker) codeRef(e *model.Element, field, ref string) {
	m := codeRefRe.FindStringSubmatch(ref)
	if m == nil {
		c.add("error", "bad_code_ref", e.File, e.ID, field, fmt.Sprintf("コードの位置 %q は 'パス:行' の形ではありません", ref), "")
		return
	}
	path := filepath.Join(c.p.Root, filepath.FromSlash(m[1]))
	lines, err := countLines(path)
	if err != nil {
		c.add("error", "code_not_found", e.File, e.ID, field, fmt.Sprintf("コード %s が見つかりません", m[1]), "対象プロジェクトのルートからの相対パスで書いてください")
		return
	}
	end, _ := strconv.Atoi(m[2])
	if m[3] != "" {
		end, _ = strconv.Atoi(m[3])
	}
	if end > lines {
		c.add("error", "code_line_out_of_range", e.File, e.ID, field, fmt.Sprintf("%s は %d 行しかありません", m[1], lines), "")
	}
}

func (c *checker) link(e *model.Element, l model.Link) {
	f := e.File
	if !c.p.Schema.HasLinkType(l.Type) {
		c.add("error", "bad_link_type", f, e.ID, "", fmt.Sprintf("リンク種別 %q は使えません", l.Type), "使える種別: "+strings.Join(c.p.Schema.LinkTypes, ", "))
	}
	if l.Target == "" {
		c.add("error", "no_link_target", f, e.ID, "", "リンクに target がありません", "")
	} else if _, ok := c.p.Elements[l.Target]; !ok {
		c.add("error", "dangling_link", f, e.ID, "", fmt.Sprintf("リンク先 %s が存在しません", l.Target), "先に要素を作るか、リンクを外してください")
	}
	if l.Source == nil {
		c.add("error", "no_source", f, e.ID, "", fmt.Sprintf("リンク %s → %s に出典がありません（リンクも主張）", l.Type, l.Target), "対応関係が書かれた情報源（トレーサビリティ表など）を source に書いてください")
	} else {
		c.source(e, "links", l.Source)
	}
}

var questionKeys = set("id", "type", "asks_for", "blocking", "to", "context", "options", "status", "answer")

func (c *checker) question(q *model.Question) {
	f := q.File
	for _, pr := range q.Problems {
		c.add("error", "shape", f, q.ID, "", pr, "")
	}
	for _, k := range q.Keys {
		if !questionKeys[k] {
			c.add("error", "unknown_key", f, q.ID, "", fmt.Sprintf("質問に定義されていないキー %q があります", k), "")
		}
	}
	if !strings.HasPrefix(q.ID, "Q-") || !idRe.MatchString(q.ID) {
		c.add("error", "bad_id", f, q.ID, "", "質問の ID は Q-数字 の形にしてください", "")
	}
	if len(q.AsksFor) == 0 {
		c.add("error", "no_asks_for", f, q.ID, "", "asks_for（この質問で埋まる空白）がありません", "'要素ID.項目名' で書いてください")
	}
	if q.Context == "" {
		c.add("error", "no_context", f, q.ID, "", "context（なぜ聞くのか）がありません", "再探索しても根拠が見つからなかった経緯を書いてください")
	}
	needBlocking := false
	for _, ref := range q.AsksFor {
		id, field, ok := strings.Cut(ref, ".")
		if !ok {
			c.add("error", "bad_asks_for", f, q.ID, "", fmt.Sprintf("asks_for %q は '要素ID.項目名' の形ではありません", ref), "")
			continue
		}
		e, exists := c.p.Elements[id]
		if !exists {
			c.add("error", "dangling_asks_for", f, q.ID, "", fmt.Sprintf("asks_for の要素 %s が存在しません", id), "")
			continue
		}
		if td := c.p.Schema.Types[e.Type]; td != nil {
			if def := td.Fields[field]; def == nil {
				c.add("error", "dangling_asks_for", f, q.ID, "", fmt.Sprintf("%s に項目 %s はありません", id, field), "")
			} else if def.Blocking {
				needBlocking = true
			}
		}
	}
	if len(q.AsksFor) > 3 {
		c.add("warning", "coarse_question", f, q.ID, "", fmt.Sprintf("1つの質問で %d 項目を聞いています。答えにくい質問になります", len(q.AsksFor)),
			"1問1判断に分けてください（1つの答えで本当に全部が決まる場合だけまとめる）")
	}
	if needBlocking && !q.Blocking {
		c.add("warning", "blocking_mismatch", f, q.ID, "", "着手条件（必須）の項目を含むのに blocking: false です", "blocking: true にしてください")
	}
	switch q.Status {
	case "open", "withdrawn":
	case "answered":
		if q.Answer == nil || q.Answer["value"] == nil || q.Answer["by"] == nil || q.Answer["at"] == nil {
			c.add("error", "answer_incomplete", f, q.ID, "", "answered には answer の value・by・at が必要です", "")
		}
	default:
		c.add("error", "bad_status", f, q.ID, "", fmt.Sprintf("質問の status %q は使えません", q.Status), "open / answered / withdrawn のいずれかにしてください")
	}
}

func countLines(path string) (int, error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer fh.Close()
	n := 0
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		n++
	}
	return n, nil
}

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
