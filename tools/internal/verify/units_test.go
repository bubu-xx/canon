package verify

import (
	"reflect"
	"testing"
)

func TestSentences(t *testing.T) {
	cases := map[string][]string{
		"A is x. B is y.":                          {"A is x.", "B is y."},
		"**Stamps.** `stamp` takes size; Vector x": {"**Stamps.**", "`stamp` takes size;", "Vector x"},
		"see e.g. Foo and (a. B) end.":             {"see e.g. Foo and (a. B) end."},
		"`a. B` c":                                 {"`a. B` c"},
		"place it.\"* Once saved":                  {"place it.\"*", "Once saved"},
		"文1。文2。":                                   {"文1。", "文2。"},
		"lower. case continues":                    {"lower. case continues"},
	}
	for in, want := range cases {
		if got := sentences(in); !reflect.DeepEqual(got, want) {
			t.Errorf("sentences(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTableUnits(t *testing.T) {
	rows := [][]string{{"F", "A", "B"}, {"---", "---", "---"}, {"x", "1", ""}, {"y", "2", "3"}}
	want := []string{"x › A: 1", "y › A: 2", "y › B: 3"}
	if got := tableUnits(rows); !reflect.DeepEqual(got, want) {
		t.Errorf("tableUnits = %q, want %q", got, want)
	}
}
