package render

import "testing"

func TestSheetName(t *testing.T) {
	cases := map[string]string{
		"BHV-005 リンクのスタンプ取得（link::stamp）": "BHV-005 リンクのスタンプ取得（link_stamp）",
		"BHV-005 リンクのスタンプ取得（link：：stamp）": "BHV-005 リンクのスタンプ取得（link_stamp）",
		"a/b＼c？d＊e［f］g":                   "a_b_c_d_e_f_g",
		"'quoted'":                        "quoted",
	}
	for in, want := range cases {
		if got := sheetName(in); got != want {
			t.Errorf("sheetName(%q) = %q, want %q", in, got, want)
		}
	}
	if n := len([]rune(sheetName("0123456789012345678901234567890123456789"))); n > 31 {
		t.Errorf("31文字を超えている: %d", n)
	}
}
