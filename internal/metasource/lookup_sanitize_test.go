package metasource

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// hostileText carries an OSC 52 clipboard write, a CSI clear-screen, a lone
// 8-bit CSI byte and a bidi override: what a malicious mirror could send.
func hostileText() string {
	return "Dune" + "\x1b]52;c;ZXZpbA==\x07" + "\x1b[2J" + "\x9b" + "2J" + "‮" + "!"
}

// fillStrings sets every string reachable from v (strings, slices and structs)
// to hostileText, so a field added to Candidate later is covered without
// touching this test.
func fillStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(hostileText())
		}
	case reflect.Slice:
		if v.CanSet() && v.Len() == 0 {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		}
		for i := 0; i < v.Len(); i++ {
			fillStrings(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fillStrings(v.Field(i))
		}
	}
}

func unsafeRunes(s string) bool {
	for _, r := range s {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

func collectStrings(v reflect.Value, path string, out map[string]string) {
	switch v.Kind() {
	case reflect.String:
		out[path] = v.String()
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectStrings(v.Index(i), path+"[]", out)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				collectStrings(v.Field(i), path+"."+v.Type().Field(i).Name, out)
			}
		}
	}
}

type hostileSource struct{}

func (hostileSource) Name() string { return "Hostile" }

func (hostileSource) Identify(context.Context, Query) ([]Candidate, error) {
	var c Candidate
	fillStrings(reflect.ValueOf(&c).Elem())
	c.Identifiers = []ebookmeta.Identifier{{Scheme: hostileText(), Value: hostileText()}}
	c.Match, c.EditionLevel = MatchTitle, true
	return []Candidate{c}, nil
}

// A source that forgets to clean what a server sent must still not reach a
// terminal: Lookup is the one place every candidate passes through.
func TestLookupCleansEveryTextFieldOfEverySource(t *testing.T) {
	res, err := Lookup(context.Background(), []Source{hostileSource{}}, Query{Title: "Dune"}, Options{MinScore: 0.01})
	if err != nil || len(res.Candidates) == 0 {
		t.Fatalf("candidates = %d, err = %v", len(res.Candidates), err)
	}
	got := map[string]string{}
	for _, c := range res.Candidates {
		collectStrings(reflect.ValueOf(c), "Candidate", got)
	}
	for path, s := range got {
		if unsafeRunes(s) || strings.Contains(s, "\x1b") {
			t.Errorf("%s reaches the caller with control text: %q", path, s)
		}
	}
}

// cleanStrings must reach text however a field holds it. Candidate has none of
// these shapes today; the walk is what keeps a future field from being missed.
func TestCleanStringsReachesMapsPointersArraysAndInterfaces(t *testing.T) {
	type holder struct {
		Map     map[string]string
		KeyMap  map[string]int
		Ptr     *string
		Array   [2]string
		Iface   any
		Nested  struct{ Inner []map[string]*string }
		private string
	}
	hostile := hostileText()
	ptr, nested := hostile, hostile
	h := holder{
		Map:    map[string]string{hostile: hostile},
		KeyMap: map[string]int{hostile: 1},
		Ptr:    &ptr,
		Array:  [2]string{hostile, hostile},
		Iface:  hostile,
	}
	h.Nested.Inner = []map[string]*string{{hostile: &nested}}
	h.private = hostile

	cleanStrings(reflect.ValueOf(&h).Elem())

	got := map[string]string{}
	collectStrings(reflect.ValueOf(&h).Elem(), "holder", got)
	for path, s := range got {
		if unsafeRunes(s) {
			t.Errorf("%s keeps control text: %q", path, s)
		}
	}
	for k, v := range h.Map {
		if unsafeRunes(k) || unsafeRunes(v) {
			t.Errorf("map entry %q: %q keeps control text", k, v)
		}
	}
	for k := range h.KeyMap {
		if unsafeRunes(k) {
			t.Errorf("map key %q keeps control text", k)
		}
	}
	for i, s := range h.Array {
		if unsafeRunes(s) {
			t.Errorf("array element %d keeps control text: %q", i, s)
		}
	}
	if unsafeRunes(*h.Ptr) || unsafeRunes(h.Iface.(string)) {
		t.Errorf("pointer %q or interface %q keeps control text", *h.Ptr, h.Iface)
	}
	for k, p := range h.Nested.Inner[0] {
		if unsafeRunes(k) || unsafeRunes(*p) {
			t.Errorf("nested entry %q: %q keeps control text", k, *p)
		}
	}
	if h.private != hostile {
		t.Error("an unexported field is not ours to change")
	}
}
