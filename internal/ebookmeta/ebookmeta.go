package ebookmeta

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Format is a detected e-book container format.
type Format string

const (
	FormatEPUB Format = "epub"
	FormatFB2  Format = "fb2"
	FormatDOCX Format = "docx"
	FormatMOBI Format = "mobi" // also AZW and AZW3 (KF8)
	FormatPDF  Format = "pdf"
)

// SupportedFormats names the formats ReadFile accepts, for messages.
const SupportedFormats = "EPUB, FB2, DOCX, MOBI, AZW3, PDF"

var (
	// ErrUnsupportedFormat means the file is not one of the known formats.
	ErrUnsupportedFormat = errors.New("unsupported e-book format")
	// ErrMalformed means the file looks like a known format but its
	// structure is damaged or outside what this package understands.
	ErrMalformed = errors.New("malformed e-book")
	// ErrEncrypted means the file is DRM-protected or password-encrypted
	// and its metadata cannot be rewritten safely.
	ErrEncrypted = errors.New("e-book is encrypted")
	// ErrNoCover means the book has no embedded cover.
	ErrNoCover = errors.New("e-book has no cover")
)

// maxPartSize caps any single part (an OPF, an XML document, a cover) that
// is read fully into memory, so a crafted file cannot exhaust it.
const maxPartSize = 64 << 20

// handler implements one format. src is the whole original file.
type handler interface {
	read(src io.ReaderAt, size int64) (*Metadata, error)
	cover(src io.ReaderAt, size int64) (*Cover, error)
	write(src io.ReaderAt, size int64, dst io.Writer, c *Changes) error
	// supports reports whether field (a Changes field name in lower case,
	// such as "series") can be stored by this format.
	supports(field string) bool
}

func handlerFor(f Format) handler {
	switch f {
	case FormatEPUB:
		return epubHandler{}
	case FormatFB2:
		return fb2Handler{}
	case FormatDOCX:
		return docxHandler{}
	case FormatMOBI:
		return mobiHandler{}
	case FormatPDF:
		return pdfHandler{}
	}
	return nil
}

// Detect identifies the format of src from its contents.
func Detect(src io.ReaderAt, size int64) (Format, error) {
	head := make([]byte, 4096)
	n, err := src.ReadAt(head, 0)
	if err != nil && err != io.EOF {
		return "", err
	}
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		return detectZip(src, size)
	case bytes.Contains(head[:min(len(head), 1024)], []byte("%PDF-")):
		return FormatPDF, nil
	case len(head) >= 68 && (string(head[60:68]) == "BOOKMOBI"):
		return FormatMOBI, nil
	case isFB2(head):
		return FormatFB2, nil
	}
	return "", ErrUnsupportedFormat
}

func detectZip(src io.ReaderAt, size int64) (Format, error) {
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	var hasTypes, hasWord bool
	for _, f := range zr.File {
		switch f.Name {
		case "mimetype":
			data, err := readZipFile(f, 256)
			if err == nil && strings.TrimSpace(string(data)) == "application/epub+zip" {
				return FormatEPUB, nil
			}
		case "META-INF/container.xml":
			return FormatEPUB, nil
		case "[Content_Types].xml":
			hasTypes = true
		case "word/document.xml":
			hasWord = true
		}
	}
	if hasTypes && hasWord {
		return FormatDOCX, nil
	}
	return "", ErrUnsupportedFormat
}

func isFB2(head []byte) bool {
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	return bytes.HasPrefix(bytes.TrimSpace(head), []byte("<")) &&
		bytes.Contains(head, []byte("<FictionBook"))
}

// identifierLimiter is implemented by formats that store only some
// identifier schemes.
type identifierLimiter interface {
	identifierSchemes() []string
}

// DropUnsupported removes from c the fields format f cannot store and
// returns their names (identifiers as "identifier:scheme"), so callers can
// tell the user instead of losing them silently.
func DropUnsupported(f Format, c *Changes) []string {
	h := handlerFor(f)
	if h == nil {
		return nil
	}
	var out []string
	drop := func(set bool, name string, clear func()) {
		if set && !h.supports(name) {
			out = append(out, name)
			clear()
		}
	}
	drop(c.Title != nil, "title", func() { c.Title = nil })
	drop(c.TitleSort != nil, "title_sort", func() { c.TitleSort = nil })
	drop(c.Authors != nil, "authors", func() { c.Authors = nil })
	drop(c.AuthorSort != nil, "author_sort", func() { c.AuthorSort = nil })
	drop(c.Publisher != nil, "publisher", func() { c.Publisher = nil })
	drop(c.Languages != nil, "languages", func() { c.Languages = nil })
	drop(c.Tags != nil, "tags", func() { c.Tags = nil })
	drop(c.Series != nil, "series", func() { c.Series = nil })
	drop(c.SeriesIndex != nil, "series_index", func() { c.SeriesIndex = nil })
	drop(c.Published != nil, "published", func() { c.Published = nil })
	drop(c.Description != nil, "description", func() { c.Description = nil })
	drop(c.Rating != nil, "rating", func() { c.Rating = nil })
	drop(c.Cover != nil, "cover", func() { c.Cover = nil })
	for _, scheme := range sortedSchemes(c.Identifiers) {
		if !CanStore(f, "identifier:"+scheme) {
			out = append(out, "identifier:"+scheme)
			delete(c.Identifiers, scheme)
		}
	}
	return out
}

// CanStore reports whether format f can hold field, named as DropUnsupported
// names it: "title", "authors", "publisher", "languages", "tags", "published",
// "description", "series", "rating", "cover" and "identifier:<scheme>".
func CanStore(f Format, field string) bool {
	h := handlerFor(f)
	if h == nil {
		return false
	}
	scheme, ok := strings.CutPrefix(field, "identifier:")
	if !ok {
		return h.supports(field)
	}
	if !h.supports("identifiers") {
		return false
	}
	if l, ok := h.(identifierLimiter); ok {
		return slices.Contains(l.identifierSchemes(), strings.ToLower(scheme))
	}
	return true
}

// Extensions lists the file extensions ReadFile can open.
var Extensions = []string{".epub", ".fb2", ".docx", ".mobi", ".azw", ".azw3", ".pdf"}

func sortedSchemes(ids map[string]string) []string {
	out := make([]string, 0, len(ids))
	for s := range ids {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// ReadFile detects path's format and returns its metadata.
func ReadFile(path string) (*Metadata, Format, error) {
	f, size, h, format, err := open(path)
	if err != nil {
		return nil, format, err
	}
	defer f.Close()
	m, err := h.read(f, size)
	if err != nil {
		return nil, format, err
	}
	m.clean()
	return m, format, nil
}

// CoverFile returns the cover embedded in path.
func CoverFile(path string) (*Cover, error) {
	f, size, h, _, err := open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return h.cover(f, size)
}

// WriteFile applies c to path. The new file is written next to the original
// and renamed over it only once complete, so a failure leaves path intact.
func WriteFile(path string, c *Changes) error {
	if c.Cover != nil {
		if err := checkCover(c.Cover); err != nil {
			return err
		}
	}
	// Replace the file a symlink points to, not the link itself.
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	f, size, h, _, err := open(path)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zlib-meta-*.part")
	if err != nil {
		f.Close()
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	writeErr := h.write(f, size, tmp, c)
	// Close the source before renaming: Windows refuses to replace a file
	// that is still open.
	f.Close()
	if writeErr != nil {
		return writeErr
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func open(path string) (*os.File, int64, handler, Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, "", err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, nil, "", err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, 0, nil, "", fmt.Errorf("%s: not a regular file", path)
	}
	format, err := Detect(f, info.Size())
	if err != nil {
		f.Close()
		return nil, 0, nil, format, err
	}
	return f, info.Size(), handlerFor(format), format, nil
}

// SniffImage returns the media type of image data, or "" when it is not a
// JPEG, PNG, GIF or WebP image.
func SniffImage(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func checkCover(c *Cover) error {
	if len(c.Data) > maxPartSize {
		return fmt.Errorf("cover image is larger than %d MiB", maxPartSize>>20)
	}
	sniffed := SniffImage(c.Data)
	if sniffed == "" {
		return errors.New("cover must be a JPEG, PNG, GIF or WebP image")
	}
	c.MediaType = sniffed
	return nil
}

// coverExt is the usual file extension for a cover media type.
func coverExt(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ".jpg"
}

// Ext is the usual file extension for c, including the dot.
func (c *Cover) Ext() string { return coverExt(c.MediaType) }

// readZipFile reads f fully, refusing more than limit bytes.
func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s is too large", ErrMalformed, f.Name)
	}
	return data, nil
}
