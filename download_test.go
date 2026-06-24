package zlib

import "testing"

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "book.epub", "book.epub"},
		{"unix traversal", "../../../../etc/passwd", "passwd"},
		{"windows traversal", `..\..\..\Windows\System32\evil.dll`, "evil.dll"},
		{"absolute path", "/etc/cron.d/job", "job"},
		{"nested dirs", "a/b/c/book.pdf", "book.pdf"},
		{"dotdot only", "..", "download"},
		{"empty", "", "download"},
		{"dot", ".", "download"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.in); got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
