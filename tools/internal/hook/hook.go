// Package hook は Claude Code のフックから呼ばれ、Canon を守る。
// 終了コード 2 で操作を止め、stderr の内容が Claude に伝わる。
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/check"
	"github.com/bubu-xx/canon/tools/internal/gaps"
	"github.com/bubu-xx/canon/tools/internal/project"
)

type input struct {
	HookEventName  string          `json:"hook_event_name"`
	Cwd            string          `json:"cwd"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	StopHookActive bool            `json:"stop_hook_active"`
}

type toolInput struct {
	FilePath   string `json:"file_path"`
	Content    string `json:"content"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
	Edits      []struct {
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	} `json:"edits"`
}

// Run は hook サブコマンドを実行し、終了コードを返す。
func Run(kind string, stdin io.Reader, stderr io.Writer, schemaFlag string) int {
	data, _ := io.ReadAll(stdin)
	var in input
	_ = json.Unmarshal(data, &in)
	// 対象プロジェクトの探し方：書き込み先ファイルの場所 → CLAUDE_PROJECT_DIR → cwd
	var starts []string
	if kind == "pre-write" {
		var ti toolInput
		_ = json.Unmarshal(in.ToolInput, &ti)
		if ti.FilePath != "" && filepath.IsAbs(ti.FilePath) {
			starts = append(starts, filepath.Dir(ti.FilePath))
		}
	}
	starts = append(starts, os.Getenv("CLAUDE_PROJECT_DIR"), in.Cwd)
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	root := ""
	for _, st := range starts {
		if st == "" {
			continue
		}
		if r, err := project.FindRoot(st); err == nil {
			root = r
			break
		}
	}
	if root == "" {
		return 0 // .canon/ が無いプロジェクトでは何もしない
	}
	switch kind {
	case "pre-write":
		return preWrite(root, in, stderr, schemaFlag)
	case "session-start":
		return sessionStart(root, schemaFlag)
	case "stop":
		return stop(root, in, stderr, schemaFlag)
	}
	fmt.Fprintf(stderr, "canon hook: 不明な種類 %s\n", kind)
	return 0
}

func preWrite(root string, in input, stderr io.Writer, schemaFlag string) int {
	var ti toolInput
	_ = json.Unmarshal(in.ToolInput, &ti)
	if ti.FilePath == "" {
		return 0
	}
	path := ti.FilePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, ok := under(filepath.Join(root, project.Dir), path)
	if !ok {
		return 0
	}
	area := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	switch area {
	case "sources":
		if _, err := os.Stat(path); err != nil && in.ToolName == "Write" {
			return 0 // 新しい情報源テキストの作成（テキスト化）は可。原文どおりかは人が差分で確認する
		}
		fmt.Fprintln(stderr, "canon: 既存の情報源（.canon/sources/）は書き換えられません。情報源は根拠そのものなので、AI が書き換えると出典の意味がなくなります。元の文書が更新されたら、人が新しい版をテキスト化して置いてください。")
		return 2
	case "views":
		fmt.Fprintln(stderr, "canon: .canon/views/ は Canon から生成される文書です。直接編集せず、Canon を直してから `canon render` を実行してください。")
		return 2
	case "canon", "questions":
	default:
		return 0
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return 0
	}
	content, err := newContent(path, ti)
	if err != nil {
		fmt.Fprintf(stderr, "canon: 変更後の内容を組み立てられません: %v\n", err)
		return 2
	}
	p, err := project.Load(root, project.SchemaDir(schemaFlag))
	if err != nil {
		fmt.Fprintf(stderr, "canon: %v\n", err)
		return 2
	}
	id, err := p.Override(path, content)
	if err != nil {
		fmt.Fprintf(stderr, "canon: %s を書き込めません: %v\n", filepath.Base(path), err)
		return 2
	}
	issues := check.Run(p, check.Options{Only: map[string]bool{id: true}, Lenient: true})
	if check.Errors(issues) == 0 {
		return 0
	}
	fmt.Fprintf(stderr, "canon: %s は Canon の規則を満たさないため書き込みを止めました。\n", filepath.Base(path))
	for _, is := range issues {
		if is.Severity == "error" {
			fmt.Fprintln(stderr, is.String())
		}
	}
	fmt.Fprintln(stderr, "原則：根拠（出典）のない値は入れない。分からなければ unknown、食い違いは conflict にして質問を起票する。")
	return 2
}

func sessionStart(root, schemaFlag string) int {
	fmt.Println("このプロジェクトには Canon（.canon/）があります。canon の原則：")
	fmt.Println("- 根拠（出典）のない値は Canon に入れない。推測で埋めない")
	fmt.Println("- 分からない項目は status: unknown、情報源が食い違う項目は status: conflict にして質問を起票する")
	fmt.Println("- 既存の情報源（.canon/sources/）は書き換えない。views/ は canon render の生成物")
	fmt.Println("- 文書・コード・人の回答は同格の情報源。食い違えば conflict、一方にしか無ければそれを根拠に確定する")
	fmt.Println("- 課題に着手する前に MCP の context_pack（または `canon context`）で材料と止まるべき条件を確認する")
	if p, err := project.Load(root, project.SchemaDir(schemaFlag)); err == nil {
		g := gaps.Build(p, false)
		fmt.Printf("現状：要素 %d、確定率 %.0f%%、必須の空白・矛盾 %d 件、未回答の質問 %d 件\n",
			len(p.Elements), g.Summary.ConfirmedRate*100, g.Summary.BlockingGaps, g.Summary.OpenQuestions)
	}
	return 0
}

func stop(root string, in input, stderr io.Writer, schemaFlag string) int {
	p, err := project.Load(root, project.SchemaDir(schemaFlag))
	if err != nil {
		return 0
	}
	issues := check.Run(p, check.Options{})
	n := check.Errors(issues)
	if n == 0 || in.StopHookActive {
		return 0
	}
	fmt.Fprintf(stderr, "canon: Canon に %d 件の規則違反が残っています。終了前に直してください。\n", n)
	shown := 0
	for _, is := range issues {
		if is.Severity != "error" {
			continue
		}
		if shown == 30 {
			fmt.Fprintln(stderr, "…（残りは `canon validate` で確認）")
			break
		}
		fmt.Fprintln(stderr, is.String())
		shown++
	}
	return 2
}

func newContent(path string, ti toolInput) ([]byte, error) {
	if ti.Content != "" || (ti.OldString == "" && len(ti.Edits) == 0) {
		return []byte(ti.Content), nil
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := string(cur)
	apply := func(old, nw string, all bool) error {
		if !strings.Contains(s, old) {
			return fmt.Errorf("置換元の文字列が見つかりません")
		}
		if all {
			s = strings.ReplaceAll(s, old, nw)
		} else {
			s = strings.Replace(s, old, nw, 1)
		}
		return nil
	}
	if ti.OldString != "" {
		if err := apply(ti.OldString, ti.NewString, ti.ReplaceAll); err != nil {
			return nil, err
		}
	}
	for _, e := range ti.Edits {
		if err := apply(e.OldString, e.NewString, e.ReplaceAll); err != nil {
			return nil, err
		}
	}
	return []byte(s), nil
}

func under(base, path string) (string, bool) {
	b := filepath.Clean(base)
	pth := filepath.Clean(path)
	rel, err := filepath.Rel(b, pth)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		// Windows で大文字小文字だけ違う場合
		if strings.HasPrefix(strings.ToLower(pth), strings.ToLower(b)+string(filepath.Separator)) {
			return pth[len(b)+1:], true
		}
		return "", false
	}
	return rel, true
}
