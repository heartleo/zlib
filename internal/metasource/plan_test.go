package metasource

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func fullCandidate() Candidate {
	c := cand("A", "Dune", []string{"Frank Herbert"}, "9780441013593")
	c.Match, c.Score = MatchISBN, 1 // a confirmed match: ISBN and title agree
	c.Publisher, c.Published, c.Description = "Ace", "2005-08-02", "A desert planet."
	c.Languages, c.Tags = []string{"en"}, []string{"Fiction", "Classics"}
	c.Identifiers = append(c.Identifiers,
		ebookmeta.Identifier{Scheme: "google", Value: "abc"},
		ebookmeta.Identifier{Scheme: "uuid", Value: "should-never-be-written"},
		ebookmeta.Identifier{Scheme: "openlibrary", Value: "OL1M"})
	return c
}

func fields(plan []Change) []string {
	var out []string
	for _, c := range plan {
		out = append(out, string(c.Field))
	}
	return out
}

func TestPlanFillsEmptyFieldsByDefault(t *testing.T) {
	cur := &ebookmeta.Metadata{Title: "Dune", Publisher: "Penguin", Tags: []string{"Space"},
		Identifiers: []ebookmeta.Identifier{{Scheme: "uuid", Value: "keep-me"}}}
	plan := Plan(cur, fullCandidate())

	var filled, replaced []string
	for _, c := range plan {
		if c.DefaultSelected() {
			filled = append(filled, string(c.Field))
		} else {
			replaced = append(replaced, string(c.Field))
		}
	}
	if want := []string{"authors", "languages", "published", "description", "identifier:isbn", "identifier:google", "identifier:openlibrary"}; !reflect.DeepEqual(filled, want) {
		t.Errorf("filled = %v, want %v", filled, want)
	}
	if want := []string{"publisher", "tags"}; !reflect.DeepEqual(replaced, want) {
		t.Errorf("needs a decision = %v, want %v", replaced, want)
	}
	for _, f := range fields(plan) {
		if f == "title" || f == "identifier:uuid" {
			t.Errorf("planned %s", f)
		}
	}

	changes := BuildChanges(plan, Change.DefaultSelected)
	if changes.Publisher != nil || changes.Tags != nil || changes.Title != nil {
		t.Errorf("default selection overwrote existing fields: %+v", changes)
	}
	if changes.Authors == nil || (*changes.Authors)[0] != "Frank Herbert" || changes.Identifiers["isbn"] != "9780441013593" {
		t.Errorf("changes = %+v", changes)
	}
	if _, ok := changes.Identifiers["uuid"]; ok {
		t.Error("uuid in changes")
	}
}

func TestPlanTagsAreAddedNotReplaced(t *testing.T) {
	cur := &ebookmeta.Metadata{Tags: []string{"Space", "fiction"}}
	plan := Plan(cur, fullCandidate())
	changes := BuildChanges(plan, func(c Change) bool { return c.Field == FieldTags })
	if want := []string{"Space", "fiction", "Classics"}; !reflect.DeepEqual(*changes.Tags, want) {
		t.Fatalf("tags = %v, want %v", *changes.Tags, want)
	}
}

func TestPlanSkipsWhatAlreadyAgrees(t *testing.T) {
	cur := &ebookmeta.Metadata{Title: "dune", Authors: []string{"Herbert, Frank"}, Publisher: "ACE", Languages: []string{"EN"},
		Published: "2005-08-02", Description: "A desert planet.", Tags: []string{"fiction", "classics"},
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "0-441-01359-7"}, {Scheme: "google", Value: "abc"}, {Scheme: "openlibrary", Value: "OL1M"}}}
	c := fullCandidate()
	c.Authors = []string{"Frank Herbert"}
	c.Identifiers[0].Value = "9780441013593"
	if plan := Plan(cur, c); len(plan) != 0 {
		t.Fatalf("plan = %v", fields(plan))
	}
}

func TestPlanLessSpecificDateIsNotAnImprovement(t *testing.T) {
	cur := &ebookmeta.Metadata{Published: "2005-08-02"}
	c := Candidate{Published: "2005"}
	if plan := Plan(cur, c); len(plan) != 0 {
		t.Fatalf("plan = %v", fields(plan))
	}
}

func TestPlanNeverProposesAnISBNFromAWorkLevelResult(t *testing.T) {
	c := fullCandidate()
	c.EditionLevel = false
	for _, f := range fields(Plan(&ebookmeta.Metadata{}, c)) {
		if f == "identifier:isbn" {
			t.Fatal("proposed an ISBN for an unknown edition")
		}
	}
}

func TestBuildChangesWithNothingSelectedIsNil(t *testing.T) {
	plan := Plan(&ebookmeta.Metadata{}, fullCandidate())
	if BuildChanges(plan, func(Change) bool { return false }) != nil {
		t.Fatal("expected nil")
	}
}

func TestPlanIgnoresAWorkLevelYearAsPublicationDate(t *testing.T) {
	c := Candidate{Title: "Dune", Published: "1965"}
	for _, f := range fields(Plan(&ebookmeta.Metadata{}, c)) {
		if f == "published" {
			t.Fatal("proposed a first-publication year as this edition's date")
		}
	}
	c.EditionLevel = true
	if got := fields(Plan(&ebookmeta.Metadata{}, c)); !reflect.DeepEqual(got, []string{"title", "published"}) {
		t.Fatalf("plan = %v", got)
	}
}

// selection lists which fields of plan start selected.
func selection(plan []Change) (on, off []string) {
	for _, c := range plan {
		if c.DefaultSelected() {
			on = append(on, string(c.Field))
		} else {
			off = append(off, string(c.Field))
		}
	}
	return on, off
}

func TestPlanForATitleOnlyMatchLeavesEditionFieldsUnselected(t *testing.T) {
	c := fullCandidate()
	c.Match, c.Score = MatchTitle, 0.82
	c.CoverURL = "https://covers.openlibrary.org/b/id/1-L.jpg"
	plan := Plan(&ebookmeta.Metadata{Title: "Dune"}, c)
	on, off := selection(plan)

	// What describes the book as a whole is still offered by default.
	for _, want := range []string{"authors", "description", "tags", "cover"} {
		if !slices.Contains(on, want) {
			t.Errorf("%s should start selected: on = %v", want, on)
		}
	}
	// What differs between editions is a guess: shown, tagged, not selected.
	for _, want := range []string{"publisher", "languages", "published", "identifier:isbn", "identifier:google", "identifier:openlibrary"} {
		if !slices.Contains(off, want) {
			t.Errorf("%s must not start selected for a title match: off = %v", want, off)
		}
	}
	for _, ch := range plan {
		wantNote := ""
		if editionField(ch.Field) {
			wantNote = NoteCheckEdition
		}
		if ch.Note != wantNote {
			t.Errorf("%s note = %q, want %q", ch.Field, ch.Note, wantNote)
		}
	}
	if changes := BuildChanges(plan, Change.DefaultSelected); changes.Publisher != nil || changes.Published != nil || len(changes.Identifiers) != 0 || changes.Languages != nil {
		t.Errorf("the default selection would write a guessed edition: %+v", changes)
	}
}

func TestPlanForAnISBNMatchWithADifferentTitleSelectsNothing(t *testing.T) {
	c := fullCandidate()
	c.Match, c.Score = MatchISBN, 0.5
	c.CoverURL = "https://covers.openlibrary.org/b/id/1-L.jpg"
	plan := Plan(&ebookmeta.Metadata{Title: "Dune"}, c)
	if len(plan) < 5 {
		t.Fatalf("the rows should still be offered: %v", fields(plan))
	}
	if on, _ := selection(plan); len(on) != 0 {
		t.Errorf("a probable other book preselected %v", on)
	}
	for _, ch := range plan {
		if ch.Note != NoteOtherBook {
			t.Errorf("%s note = %q", ch.Field, ch.Note)
		}
	}
	if BuildChanges(plan, Change.DefaultSelected) != nil {
		t.Error("the default selection would write something")
	}
}

func TestPlanTreatsAnUnscoredCandidateAsAGuess(t *testing.T) {
	c := fullCandidate()
	c.Match, c.Score = "", 0
	on, _ := selection(Plan(&ebookmeta.Metadata{Title: "Dune"}, c))
	for _, f := range on {
		if f == "publisher" || f == "published" || f == "languages" || strings.HasPrefix(f, "identifier:") {
			t.Errorf("%s preselected for a candidate with no recorded match", f)
		}
	}
}
