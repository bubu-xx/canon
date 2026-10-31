// canon は Canon（検証済みの正解情報）を検査・操作する CLI。
// フック・スキル・MCP サーバーから共通で呼ばれる。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/check"
	"github.com/bubu-xx/canon/tools/internal/crosscheck"
	"github.com/bubu-xx/canon/tools/internal/drift"
	"github.com/bubu-xx/canon/tools/internal/gaps"
	"github.com/bubu-xx/canon/tools/internal/graph"
	"github.com/bubu-xx/canon/tools/internal/hook"
	"github.com/bubu-xx/canon/tools/internal/mcpserver"
	"github.com/bubu-xx/canon/tools/internal/project"
	"github.com/bubu-xx/canon/tools/internal/render"
	"github.com/bubu-xx/canon/tools/internal/schema"
	"github.com/bubu-xx/canon/tools/internal/score"
	"github.com/bubu-xx/canon/tools/internal/verify"
)

var version = "0.1.0"

type command struct {
	name    string
	summary string
	run     func(args []string) int
}

var commands []command

func init() {
	commands = []command{
		{"init", "対象プロジェクトに .canon/ を作る", cmdInit},
		{"validate", "Canon を規則に照らして検査する（違反があれば終了コード 1）", cmdValidate},
		{"gaps", "空白・矛盾・リンク不足・未回答の質問を一覧にする", cmdGaps},
		{"drift", "観測（実装）と Canon のズレを出す", cmdDrift},
		{"render", "Canon から人向け文書を生成する", cmdRender},
		{"query", "要素を検索する", cmdQuery},
		{"impact", "要素を変えたときに見直しが要る要素を出す", cmdImpact},
		{"context", "課題に着手する前の材料一式（context_pack）を出す", cmdContext},
		{"verify", "箇所単位の照合（.canon/verify.yaml）の網羅と記録の形を検査する（不備があれば終了コード 1）", cmdVerify},
		{"crosscheck", "設計書の記述（識別子）と、同じ要素が出典にしているコードを機械的に突き合わせる", cmdCrosscheck},
		{"next-id", "型の次の ID を出す（例: canon next-id bhv）", cmdNextID},
		{"score", "取り込み結果を期待結果と比べて採点する", cmdScore},
		{"hook", "Claude Code のフックから呼ばれる（pre-write / stop）", cmdHook},
		{"mcp", "MCP サーバーとして起動する（stdio）", cmdMCP},
	}
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	name := os.Args[1]
	switch name {
	case "version", "--version", "-v":
		fmt.Println(version)
		return
	case "help", "--help", "-h":
		usage(os.Stdout)
		return
	}
	for _, c := range commands {
		if c.name == name {
			os.Exit(c.run(os.Args[2:]))
		}
	}
	fmt.Fprintf(os.Stderr, "不明なコマンド: %s\n\n", name)
	usage(os.Stderr)
	os.Exit(2)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "使い方: canon <command> [options]")
	fmt.Fprintln(w, "")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, "  %-9s %s\n", "version", "バージョンを表示する")
	fmt.Fprintln(w, "\n共通オプション: --dir <対象プロジェクト>  --schema <標準スキーマの場所>  --json")
}

type common struct {
	dir, schema string
	json        bool
}

func flags(name string, c *common) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&c.dir, "dir", ".", "対象プロジェクト（.canon/ を上へ探す起点）")
	fs.StringVar(&c.schema, "schema", "", "標準スキーマの場所（既定: プラグインの schema/）")
	fs.BoolVar(&c.json, "json", false, "JSON で出力する")
	return fs
}

func load(c common) (*project.Project, int) {
	root, err := project.FindRoot(c.dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "canon:", err)
		return nil, 2
	}
	p, err := project.Load(root, project.SchemaDir(c.schema))
	if err != nil {
		fmt.Fprintln(os.Stderr, "canon:", err)
		return nil, 2
	}
	return p, 0
}

func emit(c common, v any, text string) {
	if c.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
		return
	}
	fmt.Print(text)
}

func cmdInit(args []string) int {
	var c common
	fs := flags("init", &c)
	if fs.Parse(args) != nil {
		return 2
	}
	root, _ := filepath.Abs(c.dir)
	base := filepath.Join(root, project.Dir)
	_, statErr := os.Stat(base)
	existed := statErr == nil
	dirs := []string{"canon/req", "canon/bhv", "canon/tst", "questions", "observations", "sources", "schema-ext/types", "eval", "views"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(d)), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "canon:", err)
			return 2
		}
		keep := filepath.Join(base, filepath.FromSlash(d), ".gitkeep")
		if _, err := os.Stat(keep); err != nil {
			_ = os.WriteFile(keep, nil, 0o644)
		}
	}
	writeIfMissing := func(name, body string) {
		path := filepath.Join(base, name)
		if _, err := os.Stat(path); err != nil {
			_ = os.WriteFile(path, []byte(body), 0o644)
		}
	}
	cfg := fmt.Sprintf(`# canon の設定
schema_version: 0
project: %q
# プロジェクトの種類（スキーマのプロファイル）: business（業務システム）/ library（ライブラリ・デスクトップアプリ）
profile: business
# 対象範囲（機能名やディレクトリ）。空なら全体
scope: []
# 質問の宛先（領域 → 担当）。例: 注文: 注文チーム
question_owners: {}
`, filepath.Base(root))
	writeIfMissing("config.yaml", cfg)
	readme := `# .canon

このプロジェクトの Canon（複数の情報源を照合して確定させた、検証済みの正解情報）。

| ディレクトリ | 中身 | 書くのは |
|---|---|---|
| sources/ | 情報源（位置情報付きテキスト） | 人（AI は変更不可） |
| canon/ | Canon。値はすべて出典付き | AI（validate を通るものだけ） |
| questions/ | 空白・矛盾から生じた質問と回答 | AI が起票、人が回答 |
| observations/ | コードから観測した実装の状態 | AI |
| schema-ext/ | このプロジェクト固有のスキーマ追加 | 人 |
| eval/ | 評価用データ | 人 |
| views/ | Canon から生成した人向け文書 | canon render（手で編集しない） |
`
	writeIfMissing("README.md", readme)
	if existed {
		fmt.Printf("%s は既にあります。足りないものだけ追加しました（既存のファイルは変更していません）。\n", base)
	} else {
		fmt.Printf("%s を作成しました。\n", base)
	}
	for _, d := range dirs {
		fmt.Printf("  %s/\n", d)
	}
	return 0
}

func cmdValidate(args []string) int {
	var c common
	fs := flags("validate", &c)
	docs := fs.String("docs", "", "任意のディレクトリの YAML を検査する（スキーマの記述例など）")
	if fs.Parse(args) != nil {
		return 2
	}
	var p *project.Project
	if *docs != "" {
		sc, err := schema.Load(project.SchemaDir(c.schema), "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "canon:", err)
			return 2
		}
		p = &project.Project{Root: *docs, Schema: sc}
		p.Elements, p.Questions, p.LoadErrors = project.LoadDocs(*docs)
		p.Anchors = map[string]string{}
		p.Reorder()
	} else {
		var code int
		if p, code = load(c); p == nil {
			return code
		}
	}
	is := check.Run(p, check.Options{})
	n := check.Errors(is)
	var b strings.Builder
	for _, i := range is {
		b.WriteString(i.String() + "\n")
	}
	fmt.Fprintf(&b, "\n要素 %d・質問 %d を検査：エラー %d、警告 %d\n", len(p.Elements), len(p.Questions), n, len(is)-n)
	emit(c, map[string]any{"errors": n, "warnings": len(is) - n, "issues": is}, b.String())
	if n > 0 {
		return 1
	}
	return 0
}

func cmdGaps(args []string) int {
	var c common
	fs := flags("gaps", &c)
	blocking := fs.Bool("blocking", false, "着手条件（必須）の空白・矛盾だけを出す")
	if fs.Parse(args) != nil {
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	r := gaps.Build(p, *blocking)
	emit(c, r, r.Text())
	return 0
}

func cmdDrift(args []string) int {
	var c common
	fs := flags("drift", &c)
	head := fs.String("head", "", "現在のコミット（省略時は git から取得）")
	types := fs.String("types", "bhv", "観測の対象にする型（カンマ区切り）")
	if fs.Parse(args) != nil {
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	h := *head
	if h == "" {
		if out, err := exec.Command("git", "-C", p.Root, "rev-parse", "HEAD").Output(); err == nil {
			h = strings.TrimSpace(string(out))
		}
	}
	r := drift.Build(p, h, strings.Split(*types, ","))
	emit(c, r, r.Text())
	return 0
}

func cmdRender(args []string) int {
	var c common
	fs := flags("render", &c)
	out := fs.String("out", "", "出力先（md: ディレクトリ、既定 .canon/views ／ xlsx: ファイル、既定 .canon/views/設計書.xlsx）")
	format := fs.String("format", "md", "md（Markdown）または xlsx（Excel）")
	tmpl := fs.String("template", "", "xlsx: 流し込む先の Excel テンプレート")
	mapping := fs.String("map", "", "xlsx: テンプレートのセル対応表（YAML）")
	set := fs.String("templates", "", "xlsx: テンプレート一式のフォルダ（*.xlsx と同名の *.map.yaml）。全文書と不足一覧をまとめて出す")
	if fs.Parse(args) != nil {
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	if *format == "xlsx" && *set != "" {
		dir := *out
		if dir == "" {
			dir = filepath.Join(p.Root, project.Dir, "views", "設計書")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "canon:", err)
			return 2
		}
		maps, _ := filepath.Glob(filepath.Join(*set, "*.map.yaml"))
		sort.Strings(maps)
		var files []string
		var all strings.Builder
		all.WriteString("# 不足一覧（全文書）\n\n")
		for _, mp := range maps {
			base := strings.TrimSuffix(mp, ".map.yaml")
			outPath := filepath.Join(dir, filepath.Base(base)+".xlsx")
			cov, doc, err := render.WriteXLSXTemplate(p, base+".xlsx", mp, outPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "canon: %s: %v\n", filepath.Base(base), err)
				return 2
			}
			files = append(files, outPath)
			all.WriteString(strings.Replace(render.CoverageMarkdown(doc, cov), "# 不足一覧：", "## ", 1) + "\n")
		}
		report := filepath.Join(dir, "不足一覧.md")
		_ = os.WriteFile(report, []byte(all.String()), 0o644)
		files = append(files, report)
		var b strings.Builder
		b.WriteString("生成しました:\n")
		for _, f := range files {
			b.WriteString("  " + f + "\n")
		}
		emit(c, map[string]any{"files": files}, b.String())
		return 0
	}
	if *format == "xlsx" {
		path := *out
		if path == "" {
			name := "設計書.xlsx"
			if *tmpl != "" {
				name = strings.TrimSuffix(filepath.Base(*tmpl), filepath.Ext(*tmpl)) + "_出力.xlsx"
			}
			_ = os.MkdirAll(filepath.Join(p.Root, project.Dir, "views"), 0o755)
			path = filepath.Join(p.Root, project.Dir, "views", name)
		}
		var err error
		if *tmpl != "" {
			if *mapping == "" {
				fmt.Fprintln(os.Stderr, "canon: --template には --map（セル対応表）が必要です")
				return 2
			}
			var cov []render.Coverage
			var doc string
			cov, doc, err = render.WriteXLSXTemplate(p, *tmpl, *mapping, path)
			if err == nil {
				report := strings.TrimSuffix(path, filepath.Ext(path)) + "_不足一覧.md"
				_ = os.WriteFile(report, []byte(render.CoverageMarkdown(doc, cov)), 0o644)
				fmt.Fprintln(os.Stderr, "不足一覧: "+report)
			}
		} else {
			err = render.WriteXLSX(p, path)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "canon:", err)
			return 2
		}
		emit(c, map[string]any{"files": []string{path}}, "生成しました:\n  "+path+"\n")
		return 0
	}
	dir := *out
	if dir == "" {
		dir = filepath.Join(p.Root, project.Dir, "views")
	}
	files, err := render.Write(p, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "canon:", err)
		return 2
	}
	var b strings.Builder
	b.WriteString("生成しました:\n")
	for _, f := range files {
		b.WriteString("  " + f + "\n")
	}
	emit(c, map[string]any{"files": files}, b.String())
	return 0
}

func cmdQuery(args []string) int {
	var c common
	fs := flags("query", &c)
	typ := fs.String("type", "", "型")
	status := fs.String("status", "", "confirmed / unknown / conflict")
	text := fs.String("text", "", "検索文字列")
	if fs.Parse(args) != nil {
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	r := mcpserver.Query(p, fs.Args(), *typ, *status, *text, 1000)
	b, _ := json.MarshalIndent(r, "", "  ")
	c.json = true
	emit(c, r, string(b))
	return 0
}

func cmdImpact(args []string) int {
	var c common
	fs := flags("impact", &c)
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "使い方: canon impact <要素ID>")
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	id := fs.Arg(0)
	if _, ok := p.Elements[id]; !ok {
		fmt.Fprintf(os.Stderr, "canon: 要素 %s は Canon にありません\n", id)
		return 2
	}
	items := graph.Build(p).Impact(id)
	var b strings.Builder
	fmt.Fprintf(&b, "%s を変えると見直しが要る要素: %d 件\n", id, len(items))
	for _, it := range items {
		fmt.Fprintf(&b, "  %s %s（経路: %s）\n", it.ID, p.Elements[it.ID].Name, strings.Join(it.Path, " "))
	}
	emit(c, map[string]any{"id": id, "impacted": items}, b.String())
	return 0
}

func cmdContext(args []string) int {
	var c common
	fs := flags("context", &c)
	text := fs.String("text", "", "課題文（ID の代わりに関連要素を探す）")
	if fs.Parse(args) != nil {
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	pk := mcpserver.ContextPack(p, fs.Args(), *text)
	b, _ := json.MarshalIndent(pk, "", "  ")
	c.json = true
	emit(c, pk, string(b))
	return 0
}

func cmdNextID(args []string) int {
	var c common
	fs := flags("next-id", &c)
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "使い方: canon next-id <型 | question>")
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	t := fs.Arg(0)
	prefix := "Q"
	var ids []string
	if t == "question" || t == "q" {
		for id := range p.Questions {
			ids = append(ids, id)
		}
	} else {
		td := p.Schema.Types[t]
		if td == nil {
			fmt.Fprintf(os.Stderr, "canon: 型 %s はスキーマにありません\n", t)
			return 2
		}
		prefix = td.Prefix
		for id := range p.Elements {
			ids = append(ids, id)
		}
	}
	max := 0
	for _, id := range ids {
		var n int
		if _, err := fmt.Sscanf(id, prefix+"-%d", &n); err == nil && n > max {
			max = n
		}
	}
	fmt.Printf("%s-%03d\n", prefix, max+1)
	return 0
}

func cmdScore(args []string) int {
	var c common
	fs := flags("score", &c)
	exp := fs.String("expected", "", "期待結果のディレクトリ")
	act := fs.String("actual", "", "取り込み結果のディレクトリ（既定: .canon/canon）")
	if fs.Parse(args) != nil || *exp == "" {
		fmt.Fprintln(os.Stderr, "使い方: canon score --expected <dir> [--actual <dir>]")
		return 2
	}
	a := *act
	if a == "" {
		root, err := project.FindRoot(c.dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "canon:", err)
			return 2
		}
		a = filepath.Join(root, project.Dir, "canon")
	}
	r := score.Run(*exp, a)
	emit(c, r, r.Text())
	return 0
}

func cmdHook(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "使い方: canon hook <pre-write | stop>")
		return 2
	}
	var c common
	fs := flags("hook", &c)
	_ = fs.Parse(args[1:])
	return hook.Run(args[0], os.Stdin, os.Stderr, c.schema)
}

func cmdMCP(args []string) int {
	var c common
	fs := flags("mcp", &c)
	if fs.Parse(args) != nil {
		return 2
	}
	start := ""
	if c.dir != "." {
		start = c.dir
	}
	s := &mcpserver.Server{Version: version, SchemaFlag: c.schema, Start: start}
	if err := s.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "canon mcp:", err)
		return 1
	}
	return 0
}

func cmdCrosscheck(args []string) int {
	var c common
	fs := flags("crosscheck", &c)
	dry := fs.Bool("dry-run", false, ".canon/crosscheck.yaml を書き換えない")
	if fs.Parse(args) != nil {
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	r, err := crosscheck.Run(p, !*dry)
	if err != nil {
		fmt.Fprintln(os.Stderr, "canon:", err)
		return 2
	}
	emit(c, r, r.Text())
	return 0
}

func cmdVerify(args []string) int {
	var c common
	fs := flags("verify", &c)
	if fs.Parse(args) != nil {
		return 2
	}
	p, code := load(c)
	if p == nil {
		return code
	}
	r, err := verify.Check(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "canon:", err)
		return 2
	}
	emit(c, r, r.Text())
	if len(r.Issues) > 0 {
		return 1
	}
	return 0
}
