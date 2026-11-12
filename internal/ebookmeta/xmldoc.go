package ebookmeta

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// xnode is an element with its byte range, for splice-based editing.
type xnode struct {
	path    string // slash-joined local names from the root, e.g. "FictionBook/description"
	local   string
	attr    map[string]string // keyed by local name
	start   int64             // offset of '<'
	openEnd int64             // just after the start tag
	end     int64             // just after the end tag
	text    string            // descendant character data, trimmed
}

// xdoc indexes the elements of an XML document.
type xdoc struct {
	data     []byte
	nodes    []*xnode
	prefix   string            // element prefix of the root, "" or "p:"
	bindings map[string]string // namespace URI -> prefix declared on the root
}

// indexXML records every element for which record(path) is true, keeping
// descendant text only where keepText(path) is true.
func indexXML(data []byte, record, keepText func(path string) bool) (*xdoc, error) {
	data, err := xmlToUTF8(data)
	if err != nil {
		return nil, err
	}
	d := &xdoc{data: data, bindings: map[string]string{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = charsetReader
	type open struct {
		node *xnode
		text *strings.Builder
	}
	var stack []open
	var names []string
	for {
		off := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: XML: %v", ErrMalformed, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			names = append(names, t.Name.Local)
			path := strings.Join(names, "/")
			if len(names) == 1 {
				d.prefix = rawPrefix(data[off:])
				collectBindings(t, d.bindings)
			}
			var o open
			if record(path) {
				n := &xnode{path: path, local: t.Name.Local, attr: map[string]string{}, start: off, openEnd: dec.InputOffset()}
				for _, a := range t.Attr {
					if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
						continue
					}
					n.attr[a.Name.Local] = a.Value
				}
				d.nodes = append(d.nodes, n)
				o.node = n
				if keepText(path) {
					o.text = &strings.Builder{}
				}
			}
			stack = append(stack, o)
		case xml.CharData:
			for _, o := range stack {
				if o.text != nil {
					o.text.Write(t)
				}
			}
		case xml.EndElement:
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			names = names[:len(names)-1]
			if o.node != nil {
				o.node.end = dec.InputOffset()
				if o.text != nil {
					o.node.text = strings.TrimSpace(o.text.String())
				}
			}
		}
	}
	return d, nil
}

func (d *xdoc) all(path string) []*xnode {
	var out []*xnode
	for _, n := range d.nodes {
		if n.path == path {
			out = append(out, n)
		}
	}
	return out
}

func (d *xdoc) first(path string) *xnode {
	for _, n := range d.nodes {
		if n.path == path {
			return n
		}
	}
	return nil
}

// within returns the recorded nodes inside parent whose path is
// parent.path + "/" + rel.
func (d *xdoc) within(parent *xnode, rel string) []*xnode {
	var out []*xnode
	want := parent.path + "/" + rel
	for _, n := range d.nodes {
		if n.path == want && n.start >= parent.openEnd && n.end <= parent.end {
			out = append(out, n)
		}
	}
	return out
}

// removal deletes n with the indentation before it.
func (d *xdoc) removal(n *xnode) edit {
	s := n.start
	for s > 0 && (d.data[s-1] == ' ' || d.data[s-1] == '\t') {
		s--
	}
	if s > 0 && d.data[s-1] == '\n' {
		s--
		if s > 0 && d.data[s-1] == '\r' {
			s--
		}
	}
	return edit{start: s, end: n.end}
}

// indentOf returns the newline plus indentation in front of n, or def.
func (d *xdoc) indentOf(n *xnode, def string) string {
	i := n.start
	for i > 0 && (d.data[i-1] == ' ' || d.data[i-1] == '\t') {
		i--
	}
	if i > 0 && d.data[i-1] == '\n' {
		nl := i - 1
		if nl > 0 && d.data[nl-1] == '\r' {
			nl--
		}
		return string(d.data[nl:n.start])
	}
	return def
}

// schemaSlot inserts elements into parent so its children keep the order
// a schema requires. order lists child names in schema order.
type schemaSlot struct {
	d       *xdoc
	parent  *xnode
	order   []string
	removed map[*xnode]bool
	edits   []edit
}

// replace removes parent's children named name and inserts elems (already
// serialized) where name belongs in the schema order.
func (s *schemaSlot) replace(name string, elems ...string) {
	for _, n := range s.d.within(s.parent, name) {
		s.removed[n] = true
		s.edits = append(s.edits, s.d.removal(n))
	}
	if len(elems) == 0 {
		return
	}
	rank := func(local string) int {
		for i, o := range s.order {
			if o == local {
				return i
			}
		}
		return len(s.order)
	}
	at := s.parent.openEnd
	var anchor *xnode
	var firstChild *xnode
	for _, n := range s.d.nodes {
		if n.start < s.parent.openEnd || n.end > s.parent.end || n.path != s.parent.path+"/"+n.local {
			continue
		}
		if firstChild == nil {
			firstChild = n
		}
		if !s.removed[n] && rank(n.local) <= rank(name) {
			anchor = n
		}
	}
	indent := "\n  "
	if firstChild != nil {
		indent = s.d.indentOf(firstChild, indent)
	} else {
		indent = s.d.indentOf(s.parent, "\n") + "  "
	}
	if anchor != nil {
		at = anchor.end
	}
	s.edits = append(s.edits, edit{start: at, end: at, text: indent + strings.Join(elems, indent)})
}
