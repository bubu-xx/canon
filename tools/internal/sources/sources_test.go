package sources

import (
	"strings"
	"testing"
)

func TestAnchorKeepsTextAndNamesBlocks(t *testing.T) {
	in := "# Title\n\nIntro para.\n\n## Usage\n\n- one\n  cont\n- two\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nx := 1\n\n# not a heading\n```\n\nAfter.\n"
	out, n := Anchor(in, "doc", "2026-11-01", true)
	want := []string{
		"<!-- src: doc 2026-11-01 §冒頭 第1段落 -->\nIntro para.",
		"<!-- src: doc 2026-11-01 §Usage 項1 -->\n- one\n  cont",
		"<!-- src: doc 2026-11-01 §Usage 項2 -->\n- two",
		"<!-- src: doc 2026-11-01 §Usage 表 -->\n| a | b |",
		"<!-- src: doc 2026-11-01 §Usage コード例 -->\n```go",
		"<!-- src: doc 2026-11-01 §Usage 第1段落 -->\nAfter.",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("出力に %q がありません\n%s", w, out)
		}
	}
	if n != 6 {
		t.Errorf("アンカー %d 個（6 個のはず）", n)
	}
	// 本文の行はすべてそのまま残る
	var body []string
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "<!-- src:") {
			body = append(body, l)
		}
	}
	if strings.Join(body, "\n") != strings.TrimSuffix(in, "\n")+"\n" && strings.Join(body, "\n") != in {
		t.Errorf("本文が変わっています\n%q\n%q", strings.Join(body, "\n"), in)
	}
}

func TestDuplicateHeadings(t *testing.T) {
	out, _ := Anchor("## A\n\nx\n\n## A\n\ny\n", "d", "v", true)
	if !strings.Contains(out, "§A 第1段落（2）") {
		t.Errorf("同じ見出しのアンカーが区別されていません\n%s", out)
	}
}
