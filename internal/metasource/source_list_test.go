package metasource

import (
	"strings"
	"testing"
)

func TestNormalizeSourceList(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{" , ", ""},
		{"douban", "douban"},
		{"WeRead, douban,douban", "douban,weread"},
		{"google,openlibrary", "openlibrary,google"},
	} {
		got, err := NormalizeSourceList(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeSourceList(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	_, err := NormalizeSourceList("openlibrary,goodreads")
	if err == nil || !strings.Contains(err.Error(), `"goodreads"`) || !strings.Contains(err.Error(), "weread") {
		t.Errorf("an unknown name gave %v", err)
	}
}
