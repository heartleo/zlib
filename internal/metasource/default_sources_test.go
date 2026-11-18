package metasource

import (
	"reflect"
	"testing"
)

// A Chinese book is looked up on Douban; any other on Open Library, with Google
// Books too when a key is set.
func TestDefaultSourceNamesFollowTheBookLanguage(t *testing.T) {
	for _, tc := range []struct {
		chinese, key bool
		want         []string
	}{
		{false, false, []string{"openlibrary"}},
		{false, true, []string{"openlibrary", "google"}},
		{true, false, []string{"douban"}},
		{true, true, []string{"douban"}}, // a key adds Google for English books only
	} {
		if got := DefaultSourceNamesFor(tc.chinese, tc.key); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("DefaultSourceNamesFor(%v, %v) = %v, want %v", tc.chinese, tc.key, got, tc.want)
		}
	}
	// The shared default is never changed through a returned slice.
	DefaultSourceNamesFor(false, true)[0] = "x"
	if DefaultSourceNames[0] != "openlibrary" {
		t.Errorf("DefaultSourceNames = %v", DefaultSourceNames)
	}
}

func TestChineseBook(t *testing.T) {
	for _, tc := range []struct {
		name      string
		languages []string
		title     string
		authors   []string
		want      bool
	}{
		{"declared zh", []string{"zh"}, "Three Body", nil, true},
		{"declared zh-CN", []string{"zh-CN"}, "", nil, true},
		{"declared chi", []string{"chi"}, "", nil, true},
		{"mislabelled en, Han title", []string{"en"}, "三体", nil, true},
		{"Han author only", nil, "Three Body", []string{"刘慈欣"}, true},
		{"English", []string{"en"}, "Dune", []string{"Frank Herbert"}, false},
		{"no language, Latin", nil, "Dune", nil, false},
		{"Japanese", []string{"ja"}, "吾輩は猫である", nil, false},
		{"Korean", nil, "토지", nil, false},
	} {
		if got := ChineseBook(tc.languages, tc.title, tc.authors); got != tc.want {
			t.Errorf("%s: ChineseBook = %v, want %v", tc.name, got, tc.want)
		}
	}
}
