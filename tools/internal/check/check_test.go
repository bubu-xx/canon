package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bubu-xx/canon/tools/internal/check"
	"github.com/bubu-xx/canon/tools/internal/project"
)

// fixture は evals/fixtures/order-search を一時ディレクトリに組み立てる。
func fixture(t *testing.T) string {
	t.Helper()
	repo, _ := filepath.Abs("../../..")
	base := filepath.Join(repo, "evals", "fixtures", "order-search")
	dst := t.TempDir()
	copyDir(t, filepath.Join(base, "project"), dst)
	copyDir(t, filepath.Join(base, "expected", "canon"), filepath.Join(dst, ".canon", "canon"))
	copyDir(t, filepath.Join(base, "expected", "questions"), filepath.Join(dst, ".canon", "questions"))
	return dst
}

func load(t *testing.T, root string) *project.Project {
	t.Helper()
	repo, _ := filepath.Abs("../../..")
	p, err := project.Load(root, filepath.Join(repo, "schema"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExpectedIsValid(t *testing.T) {
	root := fixture(t)
	is := check.Run(load(t, root), check.Options{})
	if n := check.Errors(is); n != 0 {
		for _, i := range is {
			t.Log(i.String())
		}
		t.Fatalf("期待結果に %d 件のエラー", n)
	}
}

func TestRejects(t *testing.T) {
	cases := map[string]struct {
		doc  string
		code string
	}{
		"存在しない出典": {`id: REQ-009
type: req
name: x
fields:
  statement: { value: "x", status: confirmed, source: { kind: document, ref: "要件定義書 v9 §1" } }
  kind: { value: functional, status: confirmed, source: { kind: document, ref: "要件定義書 v1.3 §4" } }
  acceptance_criteria: { value: ["x"], status: confirmed, source: { kind: document, ref: "要件定義書 v1.3 §4.2.1" } }
  rationale: { status: unknown, question: Q-001 }
  priority: { value: must, status: confirmed, source: { kind: document, ref: "要件定義書 v1.3 §4.2" } }
`, "ref_not_found"},
		"出典なし": {`id: REQ-009
type: req
name: x
fields:
  statement: { value: "x", status: confirmed }
`, "no_source"},
		"推測で埋めた空白": {`id: REQ-009
type: req
name: x
fields:
  statement: { value: "x", status: unknown, question: Q-001 }
`, "unknown_with_value"},
		"たぶんの状態": {`id: REQ-009
type: req
name: x
fields:
  statement: { value: "x", status: probable }
`, "bad_status"},
		"根拠のない該当なし": {`id: REQ-009
type: req
name: x
fields:
  statement: { status: not_applicable }
`, "no_source"},
		"条件付きの必須（非機能の分類）": {`id: REQ-009
type: req
name: x
fields:
  statement: { value: "x", status: confirmed, source: { kind: document, ref: "要件定義書 v1.3 §4" } }
  kind: { value: non_functional, status: confirmed, source: { kind: document, ref: "要件定義書 v1.3 §4" } }
  acceptance_criteria: { value: ["x"], status: confirmed, source: { kind: document, ref: "要件定義書 v1.3 §4" } }
  rationale: { status: unknown, question: Q-001 }
  priority: { value: must, status: confirmed, source: { kind: document, ref: "要件定義書 v1.3 §4" } }
`, "missing_field"},
		"必須項目の欠落": {`id: REQ-009
type: req
name: x
fields: {}
`, "missing_field"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := fixture(t)
			p := load(t, root)
			path := filepath.Join(root, ".canon", "canon", "req", "REQ-009.yaml")
			id, err := p.Override(path, []byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			is := check.Run(p, check.Options{Only: map[string]bool{id: true}})
			for _, i := range is {
				if i.Code == tc.code && i.Severity == "error" {
					return
				}
			}
			var got []string
			for _, i := range is {
				got = append(got, i.Code)
			}
			t.Fatalf("%s が検出されない（検出: %s）", tc.code, strings.Join(got, ","))
		})
	}
}

func TestOneProject(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(root, ".canon", "canon", "prj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"PRJ-001", "PRJ-002"} {
		doc := "id: " + id + "\ntype: prj\nname: x\nfields: {}\n"
		if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, i := range check.Run(load(t, root), check.Options{}) {
		if i.Code == "too_many" {
			return
		}
	}
	t.Fatal("プロジェクト（PRJ）が2件あっても too_many が出ない")
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
