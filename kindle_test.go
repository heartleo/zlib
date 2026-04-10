package zlib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateKindleAttachment(t *testing.T) {
	dir := t.TempDir()
	cfg := KindleConfig{SMTPHost: "smtp.example.com"}

	allowed := []string{"book.epub", "book.pdf", "book.txt", "book.doc", "book.docx", "book.html", "book.htm", "book.rtf", "cover.jpg", "cover.jpeg", "cover.png", "cover.bmp", "cover.gif"}
	for _, name := range allowed {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
		if err := validateKindleAttachment(path, cfg); err != nil {
			t.Fatalf("validateKindleAttachment(%q) error = %v", name, err)
		}
	}

	badPath := filepath.Join(dir, "book.mobi")
	if err := os.WriteFile(badPath, []byte("test"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	err := validateKindleAttachment(badPath, cfg)
	if err == nil {
		t.Fatal("validateKindleAttachment() expected error for mobi")
	}
	if !strings.Contains(err.Error(), "unsupported Send-to-Kindle file type") {
		t.Fatalf("validateKindleAttachment() error = %v", err)
	}
}

func TestValidateKindleAttachmentRejectsOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := KindleConfig{SMTPHost: "smtp.example.com"}
	path := filepath.Join(dir, "large.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer f.Close()

	if err := f.Truncate(kindleMaxFileSizeBytes + 1); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}

	err = validateKindleAttachment(path, cfg)
	if err == nil {
		t.Fatal("validateKindleAttachment() expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "exceeds Amazon's 200 MB limit") {
		t.Fatalf("validateKindleAttachment() error = %v", err)
	}
}

func TestValidateKindleAttachmentRejectsOversizedFilesForGmail(t *testing.T) {
	dir := t.TempDir()
	cfg := KindleConfig{SMTPHost: "smtp.gmail.com"}
	path := filepath.Join(dir, "large.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer f.Close()

	// Keep the file below Amazon's cap but above Gmail's practical MIME limit.
	if err := f.Truncate(19 * 1024 * 1024); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}

	err = validateKindleAttachment(path, cfg)
	if err == nil {
		t.Fatal("validateKindleAttachment() expected Gmail size error")
	}
	if !strings.Contains(err.Error(), "exceeds Gmail's practical 25 MB message limit") {
		t.Fatalf("validateKindleAttachment() error = %v", err)
	}
}

func TestKindleConfigValidate(t *testing.T) {
	cfg := KindleConfig{
		To:       "device@kindle.com",
		From:     "user@example.com",
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestBuildKindleMessageEncodesUnicodeFilename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "三体.epub")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := KindleConfig{
		To:       "device@kindle.com",
		From:     "user@example.com",
		SMTPHost: "smtp.gmail.com",
		SMTPPort: 587,
	}

	msg, err := buildKindleMessage(path, cfg)
	if err != nil {
		t.Fatalf("buildKindleMessage() error = %v", err)
	}

	text := string(msg)
	if !strings.Contains(text, "filename*=UTF-8''%E4%B8%89%E4%BD%93.epub") {
		t.Fatalf("buildKindleMessage() missing RFC 2231 filename encoding: %s", text)
	}
	if !strings.Contains(text, "filename=\"attachment.epub\"") {
		t.Fatalf("buildKindleMessage() missing ASCII fallback filename: %s", text)
	}
}
