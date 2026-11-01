// Package sources は、テキストの文書（Markdown・プレーンテキスト）を、原文を変えずに
// 出典の位置（アンカー）付きで .canon/sources/ に取り込む。
//
// 位置は機械的に決める：直近の見出し（無ければ「冒頭」）＋その中のブロックの種類と番号
// （第N段落・項N・表・コード例・引用）。同じ文書を同じ版で取り込めば、必ず同じアンカーになる。
package sources

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Result は1ファイルの取り込み結果。
type Result struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Anchors int    `json:"anchors"`
	Skipped string `json:"skipped,omitempty"`
}

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
var listRe = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)

// Name は文書名（プロジェクトのルートからの相対パス、拡張子なし、/ は -）。
func Name(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path) // プロジェクトの外のファイルはファイル名だけ
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	return strings.ReplaceAll(rel, "/", "-")
}

// Version は版。git の最終コミット日（YYYY-MM-DD）、取れなければ更新日。
func Version(path string) string {
	cmd := exec.Command("git", "-C", filepath.Dir(path), "log", "-1", "--format=%cs", "--", filepath.Base(path))
	if out, err := cmd.Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}
	if st, err := os.Stat(path); err == nil {
		return st.ModTime().Format("2006-01-02")
	}
	return "unknown"
}

// Import は path を root（対象プロジェクトのルート）からの名前で outDir に取り込む。既にあれば上書きしない。
func Import(root, outDir, path, version string) (Result, error) {
	name := Name(root, path)
	if version == "" {
		version = Version(path)
	}
	out := filepath.Join(outDir, name+"_"+version+".md")
	r := Result{From: path, To: out}
	if _, err := os.Stat(out); err == nil {
		r.Skipped = "同じ版が既にある"
		return r, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	rel, _ := filepath.Rel(root, path)
	text, n := Anchor(string(data), name, version, strings.EqualFold(filepath.Ext(path), ".md"))
	r.Anchors = n
	head := "<!-- 元ファイル: " + filepath.ToSlash(rel) + " を canon sources import で取り込み（本文は原文どおり） -->\n\n"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return r, err
	}
	return r, os.WriteFile(out, []byte(head+text), 0o644)
}

// Anchor は本文の各ブロックの前にアンカーを入れる。本文の行は変えない。
func Anchor(text, name, version string, markdown bool) (string, int) {
	var b strings.Builder
	sc := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(text, "\r\n", "\n")))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	section := "冒頭"
	counts := map[string]int{}
	used := map[string]int{}
	n := 0
	inCode := false
	prevBlank := true
	inList := false
	inTable := false
	emit := func(kind string) {
		key := section + "\x00" + kind
		counts[key]++
		pos := kind
		switch kind {
		case "段落":
			pos = fmt.Sprintf("第%d段落", counts[key])
		case "項":
			pos = fmt.Sprintf("項%d", counts[key])
		default:
			if counts[key] > 1 {
				pos = fmt.Sprintf("%s%d", kind, counts[key])
			}
		}
		ref := name + " " + version + " §" + section + " " + pos
		used[ref]++
		if used[ref] > 1 {
			ref += fmt.Sprintf("（%d）", used[ref])
		}
		b.WriteString("<!-- src: " + ref + " -->\n")
		n++
	}
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		if markdown && strings.HasPrefix(t, "```") || markdown && strings.HasPrefix(t, "~~~") {
			if !inCode {
				emit("コード例")
				inList, inTable = false, false
			}
			inCode = !inCode
			b.WriteString(line + "\n")
			prevBlank = false
			continue
		}
		if inCode {
			b.WriteString(line + "\n")
			continue
		}
		if t == "" {
			b.WriteString(line + "\n")
			prevBlank = true
			inTable = false
			continue
		}
		if markdown {
			if m := headingRe.FindStringSubmatch(t); m != nil && !strings.HasPrefix(line, " ") {
				section = cleanHeading(m[2])
				if len(m[1]) == 1 {
					section = "冒頭"
				}
				counts = map[string]int{}
				b.WriteString(line + "\n")
				prevBlank, inList, inTable = true, false, false
				continue
			}
			indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
			switch {
			case strings.HasPrefix(t, "|"):
				if !inTable {
					emit("表")
					inTable, inList = true, false
				}
			case listRe.MatchString(t) && !indented:
				emit("項")
				inList, inTable = true, false
			case strings.HasPrefix(t, ">"):
				if prevBlank {
					emit("引用")
					inList, inTable = false, false
				}
			case prevBlank && !(inList && indented):
				emit("段落")
				inList, inTable = false, false
			}
		} else if prevBlank {
			emit("段落")
		}
		b.WriteString(line + "\n")
		prevBlank = false
	}
	return b.String(), n
}

func cleanHeading(s string) string {
	s = strings.NewReplacer("`", "", "*", "", "<!--", "", "-->", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
