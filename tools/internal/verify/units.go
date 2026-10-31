package verify

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/model"
)

// 照合の区切り（unit）は、情報源のテキストから機械的に作る。
// エージェントが文の分け方を自分で決めると、回ごとに細かさが変わり、照合の深さも結果の突き合わせも揺れる。
// 区切りを道具で固定し、エージェントは区切りごとに判定を埋める（区切りをさらに分けるのはよいが、まとめてはいけない）。
//
// 区切り方：
//   - 表：データ行ごと、列ごとに1つ（「1列目の値 › 見出し: セル」）
//   - 箇条書き：項目ごと。項目の中は文ごと
//   - 段落・引用：文ごと（英語は . ! ? の後で次が大文字・記号のとき、日本語は 。、どちらも ; でも分ける）
//   - コードブロック：全体で1つ

var srcLineRe = regexp.MustCompile(`^\s*<!--\s*src:\s*(.+?)\s*-->\s*$`)
var listItemRe = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)

// Units は情報源のアンカーごとの区切りを返す（アンカーは正規化済み）。
func Units(sourcesDir string) map[string][]string {
	out := map[string][]string{}
	_ = filepath.WalkDir(sourcesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
		var cur string
		var block []string
		flush := func() {
			if cur != "" {
				out[cur] = append(out[cur], splitBlock(block)...)
			}
			block = nil
		}
		inCode := false
		for sc.Scan() {
			line := sc.Text()
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "```") {
				inCode = !inCode
				block = append(block, line)
				continue
			}
			if !inCode {
				if m := srcLineRe.FindStringSubmatch(line); m != nil {
					flush()
					cur = model.NormalizeRef(strings.ReplaceAll(m[1], "|", " "))
					continue
				}
				if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "<!--") {
					flush()
					cur = ""
					continue
				}
			}
			block = append(block, line)
		}
		flush()
		return nil
	})
	return out
}

// AnchorOrder は区切りのあるアンカーを並べて返す。
func AnchorOrder(units map[string][]string) []string {
	var as []string
	for a := range units {
		as = append(as, a)
	}
	sort.Strings(as)
	return as
}

func splitBlock(lines []string) []string {
	var out []string
	var para []string
	var item []string
	var table [][]string
	var code []string
	inCode := false
	endPara := func() {
		if len(para) > 0 {
			out = append(out, sentences(strings.Join(para, " "))...)
			para = nil
		}
	}
	endItem := func() {
		if len(item) > 0 {
			out = append(out, sentences(strings.Join(item, " "))...)
			item = nil
		}
	}
	endTable := func() {
		if len(table) > 0 {
			out = append(out, tableUnits(table)...)
			table = nil
		}
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			if inCode {
				code = append(code, line)
				out = append(out, strings.Join(code, "\n"))
				code = nil
				inCode = false
			} else {
				endPara()
				endItem()
				endTable()
				inCode = true
				code = []string{line}
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if t == "" {
			endPara()
			endItem()
			endTable()
			continue
		}
		if strings.HasPrefix(t, "|") {
			endPara()
			endItem()
			table = append(table, cells(t))
			continue
		}
		endTable()
		t = strings.TrimSpace(strings.TrimPrefix(t, ">"))
		if listItemRe.MatchString(t) {
			endPara()
			endItem()
			item = []string{listItemRe.ReplaceAllString(t, "")}
			continue
		}
		if len(item) > 0 {
			item = append(item, t)
			continue
		}
		para = append(para, t)
	}
	if inCode && len(code) > 0 {
		out = append(out, strings.Join(code, "\n"))
	}
	endPara()
	endItem()
	endTable()
	var clean []string
	for _, u := range out {
		if strings.TrimSpace(u) != "" {
			clean = append(clean, u)
		}
	}
	return clean
}

func cells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	var cs []string
	var b strings.Builder
	tick := false
	for _, r := range row {
		switch {
		case r == '`':
			tick = !tick
			b.WriteRune(r)
		case r == '|' && !tick:
			cs = append(cs, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	cs = append(cs, strings.TrimSpace(b.String()))
	return cs
}

var sepRe = regexp.MustCompile(`^:?-{2,}:?$`)

func isSeparator(cs []string) bool {
	for _, c := range cs {
		if c != "" && !sepRe.MatchString(c) {
			return false
		}
	}
	return true
}

func tableUnits(rows [][]string) []string {
	if len(rows) == 0 {
		return nil
	}
	head := rows[0]
	body := rows[1:]
	if len(body) > 0 && isSeparator(body[0]) {
		body = body[1:]
	} else {
		head = nil
		body = rows
	}
	var out []string
	for _, r := range body {
		if len(r) == 1 || head == nil {
			out = append(out, strings.Join(r, " | "))
			continue
		}
		for j := 1; j < len(r); j++ {
			if r[j] == "" {
				continue
			}
			h := ""
			if j < len(head) {
				h = head[j]
			}
			out = append(out, r[0]+" › "+h+": "+r[j])
		}
	}
	return out
}

var abbrevs = []string{"e.g.", "i.e.", "etc.", "vs.", "cf.", "approx."}

// sentences は文に分ける。バッククォートと括弧の中では分けない。
func sentences(s string) []string {
	s = strings.Join(strings.Fields(s), " ")
	rs := []rune(s)
	var out []string
	start := 0
	tick := false
	depth := 0
	cut := func(end int) {
		piece := strings.TrimSpace(string(rs[start:end]))
		if piece != "" {
			out = append(out, piece)
		}
		start = end
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '`':
			tick = !tick
			continue
		case '(', '（', '「', '[':
			if !tick {
				depth++
			}
			continue
		case ')', '）', '」', ']':
			if !tick && depth > 0 {
				depth--
			}
			continue
		}
		if tick || depth > 0 {
			continue
		}
		switch r {
		case '。':
			cut(i + 1)
		case ';', '；':
			cut(i + 1)
		case '.', '!', '?':
			j := i + 1
			for j < len(rs) && strings.ContainsRune("*_\"'”’", rs[j]) {
				j++
			}
			if j+1 >= len(rs) || rs[j] != ' ' {
				continue
			}
			next := rs[j+1]
			if !(next >= 'A' && next <= 'Z') && !strings.ContainsRune("`*\"'(_", next) {
				continue
			}
			word := string(rs[lastSpace(rs, i)+1 : i+1])
			skip := false
			for _, a := range abbrevs {
				if strings.EqualFold(word, a) {
					skip = true
				}
			}
			if !skip {
				cut(j)
				i = j - 1
			}
		}
	}
	cut(len(rs))
	return out
}

func lastSpace(rs []rune, i int) int {
	for j := i; j >= 0; j-- {
		if rs[j] == ' ' {
			return j
		}
	}
	return -1
}
