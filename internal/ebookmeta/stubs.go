package ebookmeta

import "io"

type stub struct{}

func (stub) supports(string) bool { return false }
func (stub) read(io.ReaderAt, int64) (*Metadata, error) {
	return nil, ErrUnsupportedFormat
}
func (stub) cover(io.ReaderAt, int64) (*Cover, error) { return nil, ErrUnsupportedFormat }
func (stub) write(io.ReaderAt, int64, io.Writer, *Changes) error {
	return ErrUnsupportedFormat
}

type fb2Handler struct{ stub }
type docxHandler struct{ stub }
type mobiHandler struct{ stub }
type pdfHandler struct{ stub }
