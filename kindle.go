package zlib

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type KindleConfig struct {
	To       string `json:"to"`
	From     string `json:"from"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
}

const (
	kindleMaxFileSizeBytes     = 200 * 1024 * 1024
	gmailMaxMessageSizeBytes   = 25 * 1024 * 1024
	base64LineLength           = 76
	base64LineBreakLength      = 2
	kindleEstimatedHeaderBytes = 2048
)

func (c KindleConfig) Validate() error {
	if strings.TrimSpace(c.To) == "" {
		return fmt.Errorf("kindle to address is required")
	}
	if strings.TrimSpace(c.From) == "" {
		return fmt.Errorf("kindle from address is required")
	}
	if strings.TrimSpace(c.SMTPHost) == "" {
		return fmt.Errorf("kindle smtp host is required")
	}
	if c.SMTPPort <= 0 {
		return fmt.Errorf("kindle smtp port must be greater than 0")
	}
	return nil
}

func SendToKindle(filePath string, cfg KindleConfig, smtpPassword string) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(smtpPassword) == "" {
		return fmt.Errorf("smtp password is required")
	}
	if err := validateKindleAttachment(filePath, cfg); err != nil {
		return err
	}

	msg, err := buildKindleMessage(filePath, cfg)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	auth := smtp.PlainAuth("", cfg.From, smtpPassword, cfg.SMTPHost)
	if err := smtp.SendMail(addr, auth, cfg.From, []string{cfg.To}, msg); err != nil {
		return fmt.Errorf("smtp send failed: %w", err)
	}
	return nil
}

func validateKindleAttachment(filePath string, cfg KindleConfig) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".epub", ".pdf", ".txt", ".doc", ".docx", ".html", ".htm", ".rtf", ".jpg", ".jpeg", ".png", ".bmp", ".gif":
	case "":
		return fmt.Errorf("send to kindle failed: file has no extension")
	default:
		return fmt.Errorf("send to kindle failed: unsupported Send-to-Kindle file type %s", ext)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("send to kindle failed: %w", err)
	}
	if info.Size() > kindleMaxFileSizeBytes {
		return fmt.Errorf("send to kindle failed: file size %.2f MB exceeds Amazon's 200 MB limit", float64(info.Size())/1024.0/1024.0)
	}
	if isGmailSMTPHost(cfg.SMTPHost) {
		estimatedMessageSize := estimateBase64MessageSize(info.Size()) + kindleEstimatedHeaderBytes
		if estimatedMessageSize > gmailMaxMessageSizeBytes {
			return fmt.Errorf("send to kindle failed: file size %.2f MB exceeds Gmail's practical 25 MB message limit for attachments", float64(info.Size())/1024.0/1024.0)
		}
	}

	return nil
}

func isGmailSMTPHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "smtp.gmail.com" || host == "smtp.googlemail.com"
}

func estimateBase64MessageSize(fileSize int64) int64 {
	if fileSize <= 0 {
		return 0
	}
	encodedLen := ((fileSize + 2) / 3) * 4
	lineCount := (encodedLen + base64LineLength - 1) / base64LineLength
	return encodedLen + lineCount*base64LineBreakLength
}

func buildKindleMessage(filePath string, cfg KindleConfig) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	filename := filepath.Base(filePath)
	const boundary = "zlib-kindle-boundary"

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", cfg.From)
	fmt.Fprintf(&buf, "To: %s\r\n", cfg.To)
	fmt.Fprintf(&buf, "Subject: Send to Kindle\r\n")
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary)

	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	fmt.Fprintf(&buf, "Sent by zlib-cli.\r\n\r\n")

	attachmentHeader := buildAttachmentHeader(filename)

	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: application/octet-stream; %s\r\n", attachmentHeader)
	fmt.Fprintf(&buf, "Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&buf, "Content-Disposition: attachment; %s\r\n\r\n", attachmentHeader)

	encoded := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		fmt.Fprintf(&buf, "%s\r\n", encoded[i:end])
	}

	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes(), nil
}

func buildAttachmentHeader(filename string) string {
	if isASCII(filename) {
		return fmt.Sprintf("filename=%q", filename)
	}

	fallback := asciiFilenameFallback(filename)
	encoded := url.PathEscape(filename)
	return fmt.Sprintf("filename=%q; filename*=UTF-8''%s", fallback, encoded)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func asciiFilenameFallback(filename string) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	var b strings.Builder
	for _, r := range base {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		case r <= unicode.MaxASCII && (r == '-' || r == '_' || r == '.'):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}

	fallback := strings.Trim(b.String(), "._")
	if fallback == "" {
		fallback = "attachment"
	}

	if ext == "" {
		return fallback
	}
	return fallback + ext
}
