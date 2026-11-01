package gaps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFunctions(t *testing.T) {
	dir := t.TempDir()
	src := "pub fn a() {}\nimpl X {\n    pub(crate) async fn b(&self) {}\n    fn c() {}\n}\nconst unsafe fn d() {}\n// fn not_code\nlet f = |x| x;\n"
	p := filepath.Join(dir, "x.rs")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got := functions(p, funcRes[".rs"])
	want := []fnDef{{"a", 1}, {"b", 3}, {"c", 4}, {"d", 6}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got[i], want[i])
		}
	}
	ts := filepath.Join(dir, "x.ts")
	_ = os.WriteFile(ts, []byte("export function f() {}\nexport const g = async (a: number): Promise<void> => {}\nconst h = 3\n"), 0o644)
	if got := functions(ts, funcRes[".ts"]); len(got) != 2 || got[0].name != "f" || got[1].name != "g" {
		t.Errorf("ts: %v", got)
	}
}

func TestIgnoredCode(t *testing.T) {
	ig := []IgnoredCode{{Path: "video/generated/**", By: "x"}, {Path: "*.pb.go", By: "x"}, {Path: "light/tools", By: "x"}}
	for rel, want := range map[string]bool{
		"video/generated/a/b.rs": true, "video/src/a.rs": false, "photo/x.pb.go": true,
		"light/tools/t.rs": true, "light/toolsx/t.rs": false,
	} {
		if got := isIgnoredCode(rel, ig); got != want {
			t.Errorf("%s: got %v, want %v", rel, got, want)
		}
	}
}
