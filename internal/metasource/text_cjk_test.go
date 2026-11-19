package metasource

import "testing"

func TestFoldTextKeepsVolumeNumerals(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"三体Ⅱ", "三体2"}, // Roman numeral glyph and Arabic digit agree
		{"三体Ⅱ", "三体２"}, // full-width digit
		{"卷ⅻ", "卷12"},  // glyphs above Ⅸ expand to two digits
		{"三体ⅱ", "三体Ⅱ"}, // small and capital glyphs agree
	} {
		if string(foldText(tc.a)) != string(foldText(tc.b)) {
			t.Errorf("foldText(%q) = %q, foldText(%q) = %q; want equal", tc.a, string(foldText(tc.a)), tc.b, string(foldText(tc.b)))
		}
	}
	// A sequel is not the first volume.
	if string(foldText("三体Ⅱ")) == string(foldText("三体")) {
		t.Error("三体Ⅱ folds to the same key as 三体")
	}
	if s := titleSimilarity("三体", "三体Ⅱ"); s >= 0.9 {
		t.Errorf("titleSimilarity(三体, 三体Ⅱ) = %v, want a clear gap from an exact match", s)
	}
	if s := titleSimilarity("三体Ⅱ", "三体2"); s != 1 {
		t.Errorf("titleSimilarity(三体Ⅱ, 三体2) = %v, want 1", s)
	}
}

func TestNormalizeDateChinese(t *testing.T) {
	for in, want := range map[string]string{
		"2008年1月":    "2008-01",
		"2008年1月5日":  "2008-01-05",
		"2008年":      "2008",
		"2008-1":     "2008-01",
		"2016年12月":   "2016-12",
		"二〇〇八年":      "",
		"2008年13月":   "2008",
		"出版于2008年1月": "2008-01",
	} {
		if got := normalizeDate(in); got != want {
			t.Errorf("normalizeDate(%q) = %q, want %q", in, got, want)
		}
	}
}
