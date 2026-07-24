package redact

import (
	"strings"
	"testing"

	"github.com/letsgolearn/p2data/internal/pfilter"
)

func ents(es ...pfilter.Entity) []pfilter.Entity { return es }

func TestApply_DefaultTag(t *testing.T) {
	r := New("", nil)
	text := "Email me at jane@acme.com please"
	start := strings.Index(text, "jane@acme.com")
	got, applied := r.Apply(text, ents(pfilter.Entity{Start: start, End: start + len("jane@acme.com"), Label: "private_email", Score: 1}), Policy{})
	want := "Email me at [EMAIL] please"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if len(applied) != 1 {
		t.Fatalf("applied=%d want 1", len(applied))
	}
}

func TestApply_KeepFirstPerson(t *testing.T) {
	r := New("", nil)
	text := "Email Jane Doe at jane@acme.com"
	pStart := strings.Index(text, "Jane Doe")
	eStart := strings.Index(text, "jane@acme.com")
	es := ents(
		pfilter.Entity{Start: pStart, End: pStart + len("Jane Doe"), Label: "private_person", Score: 1},
		pfilter.Entity{Start: eStart, End: eStart + len("jane@acme.com"), Label: "private_email", Score: 1},
	)
	p := Policy{ByLabel: map[string]Mode{"private_person": ModeKeepFirst}}
	got, _ := r.Apply(text, es, p)
	want := "Email Jane [LAST] at [EMAIL]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApply_KeepFirstCommaName(t *testing.T) {
	r := New("", nil)
	text := "Student: Diede, Anderson Cash (grade 3)"
	s := strings.Index(text, "Diede")
	e := pfilter.Entity{Start: s, End: s + len("Diede, Anderson Cash"), Label: "private_person"}
	got, _ := r.Apply(text, ents(e), Policy{Default: ModeKeepFirst})
	want := "Student: Anderson [LAST] (grade 3)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApply_KeepFirstSingleToken(t *testing.T) {
	r := New("", nil)
	text := "Hi Jane"
	s := strings.Index(text, "Jane")
	got, _ := r.Apply(text, ents(pfilter.Entity{Start: s, End: s + 4, Label: "private_person"}), Policy{Default: ModeKeepFirst})
	if got != "Hi Jane" {
		t.Fatalf("single-token name should be unchanged, got %q", got)
	}
}

func TestApply_MaskAndDrop(t *testing.T) {
	r := New("###", nil)
	text := "a SECRETVAL b"
	s := strings.Index(text, "SECRETVAL")
	e := pfilter.Entity{Start: s, End: s + len("SECRETVAL"), Label: "secret"}

	got, _ := r.Apply(text, ents(e), Policy{Default: ModeMask})
	if got != "a ### b" {
		t.Fatalf("mask: got %q", got)
	}
	got, _ = r.Apply(text, ents(e), Policy{Default: ModeDrop})
	if got != "a  b" {
		t.Fatalf("drop: got %q", got)
	}
}

func TestApply_HashStable(t *testing.T) {
	r := New("", []byte("k"))
	text := "x foo@bar.com y foo@bar.com z"
	var es []pfilter.Entity
	idx := 0
	for {
		i := strings.Index(text[idx:], "foo@bar.com")
		if i < 0 {
			break
		}
		s := idx + i
		es = append(es, pfilter.Entity{Start: s, End: s + len("foo@bar.com"), Label: "private_email"})
		idx = s + len("foo@bar.com")
	}
	got, _ := r.Apply(text, es, Policy{Default: ModeHash})
	// both occurrences hash to the same token
	parts := strings.Fields(got)
	if parts[1] != parts[3] {
		t.Fatalf("hash not stable: %q vs %q (%q)", parts[1], parts[3], got)
	}
	if !strings.HasPrefix(parts[1], "EMAIL_") {
		t.Fatalf("hash token missing tag prefix: %q", parts[1])
	}
}

func TestApply_AllowListSkipsOthers(t *testing.T) {
	r := New("", nil)
	text := "Jane at jane@acme.com"
	pStart := strings.Index(text, "Jane")
	eStart := strings.Index(text, "jane@acme.com")
	es := ents(
		pfilter.Entity{Start: pStart, End: pStart + 4, Label: "private_person"},
		pfilter.Entity{Start: eStart, End: eStart + len("jane@acme.com"), Label: "private_email"},
	)
	// Only redact emails; the person should pass through.
	p := Policy{Labels: []string{"private_email"}}
	got, applied := r.Apply(text, es, p)
	if got != "Jane at [EMAIL]" {
		t.Fatalf("got %q", got)
	}
	if len(applied) != 1 || applied[0].Label != "private_email" {
		t.Fatalf("applied=%+v", applied)
	}
}

func TestApply_OverlapAndOutOfRange(t *testing.T) {
	r := New("", nil)
	text := "abcdef"
	es := ents(
		pfilter.Entity{Start: 1, End: 4, Label: "secret"},  // kept
		pfilter.Entity{Start: 2, End: 5, Label: "secret"},  // overlaps -> skipped
		pfilter.Entity{Start: 5, End: 99, Label: "secret"}, // out of range -> skipped
	)
	got, applied := r.Apply(text, es, Policy{})
	if got != "a[SECRET]ef" {
		t.Fatalf("got %q", got)
	}
	if len(applied) != 1 {
		t.Fatalf("applied=%d want 1", len(applied))
	}
}

func TestApply_ParentLabelCoversDateOfBirth(t *testing.T) {
	r := New("", nil)
	text := "Birthdate 04/24/2015 reviewed"
	s := strings.Index(text, "04/24/2015")
	e := pfilter.Entity{Start: s, End: s + len("04/24/2015"), Label: "date_of_birth"}

	// An allow-list with only the parent label still redacts the subtype, so a
	// pre-date_of_birth policy cannot leak a birthdate.
	got, _ := r.Apply(text, ents(e), Policy{Labels: []string{"private_date"}})
	if got != "Birthdate [DOB] reviewed" {
		t.Fatalf("parent allow-list: got %q", got)
	}

	// A byLabel override on the parent applies to the subtype too.
	got, _ = r.Apply(text, ents(e), Policy{ByLabel: map[string]Mode{"private_date": ModeDrop}})
	if got != "Birthdate  reviewed" {
		t.Fatalf("parent byLabel: got %q", got)
	}

	// The reverse does not hold: allowing only date_of_birth leaves a general
	// date untouched.
	gen := pfilter.Entity{Start: s, End: s + len("04/24/2015"), Label: "private_date"}
	got, _ = r.Apply(text, ents(gen), Policy{Labels: []string{"date_of_birth"}})
	if got != text {
		t.Fatalf("general date should pass through: got %q", got)
	}
}

func TestApplyParts_MapsEntitiesBackToParts(t *testing.T) {
	r := New("", nil)
	// Mimics HTML text nodes: the label and its value arrive as separate
	// strings; classification runs over the joined document.
	parts := []string{"Birthdate", "04/24/2015", "Grade", "03"}
	joined := JoinParts(parts)

	dob := strings.Index(joined, "04/24/2015")
	grade := strings.LastIndex(joined, "03")
	es := ents(
		pfilter.Entity{Start: dob, End: dob + len("04/24/2015"), Label: "date_of_birth", Score: 1},
		pfilter.Entity{Start: grade, End: grade + len("03"), Label: "private_grade", Score: 1},
	)

	out, applied := r.ApplyParts(parts, es, Policy{})
	want := []string{"Birthdate", "[DOB]", "Grade", "[GRADE]"}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("part %d: got %q want %q", i, out[i], want[i])
		}
	}
	if len(applied) != 2 {
		t.Fatalf("applied=%d want 2", len(applied))
	}
}

func TestApplyParts_EntityCrossingBoundary(t *testing.T) {
	r := New("", nil)
	parts := []string{"call Jane", "Doe now"}
	joined := JoinParts(parts) // "call Jane\n\nDoe now"

	s := strings.Index(joined, "Jane")
	e := strings.Index(joined, "Doe") + len("Doe")
	es := ents(pfilter.Entity{Start: s, End: e, Label: "private_person", Score: 1})

	out, _ := r.ApplyParts(parts, es, Policy{})
	// The replacement lands in the part where the entity starts; the covered
	// text in the next part (and the separator glue) is dropped.
	if out[0] != "call [PERSON]" {
		t.Errorf("part 0: got %q", out[0])
	}
	if out[1] != " now" {
		t.Errorf("part 1: got %q", out[1])
	}
}

func TestApplyParts_AllowListLeavesExcludedInPlace(t *testing.T) {
	r := New("", nil)
	parts := []string{"Meeting on", "04/12/2024"}
	joined := JoinParts(parts)
	s := strings.Index(joined, "04/12/2024")
	es := ents(pfilter.Entity{Start: s, End: s + len("04/12/2024"), Label: "private_date", Score: 1})

	out, applied := r.ApplyParts(parts, es, Policy{Labels: []string{"date_of_birth"}})
	if out[0] != "Meeting on" || out[1] != "04/12/2024" {
		t.Errorf("got %q", out)
	}
	if len(applied) != 0 {
		t.Fatalf("applied=%d want 0", len(applied))
	}
}

func TestApply_ProfessionalReferencesKept(t *testing.T) {
	r := New("", nil)

	for _, tc := range []struct {
		text string
		span string // the private_person span the classifier reports
	}{
		// Title inside the span.
		{"Reference full psychoeducational report of Dr. Paul Smith dated 05/11/2023.", "Dr. Paul Smith"},
		// Title just outside the span (model tags only the name).
		{"Evaluated by Dr. Paul Smith on site.", "Paul Smith"},
		{"Seen by Professor Jane Doe last week.", "Jane Doe"},
		// Credential suffix.
		{"Report signed by Jane Smith, Ph.D. on file.", "Jane Smith"},
		{"Assessment by John Roe, BCBA completed.", "John Roe"},
	} {
		s := strings.Index(tc.text, tc.span)
		e := pfilter.Entity{Start: s, End: s + len(tc.span), Label: "private_person", Score: 1}
		got, applied := r.Apply(tc.text, ents(e), Policy{ByLabel: map[string]Mode{"private_person": ModeKeepFirst}})
		if got != tc.text {
			t.Errorf("professional reference was redacted: %q -> %q", tc.text, got)
		}
		if len(applied) != 0 {
			t.Errorf("applied=%d want 0 for %q", len(applied), tc.text)
		}
	}
}

func TestApply_NonProfessionalPersonsStillRedacted(t *testing.T) {
	r := New("", nil)

	for _, tc := range []struct {
		text string
		span string
	}{
		// No title: the student or a plain name.
		{"Student Paul Smith continues to qualify.", "Paul Smith"},
		// Social titles refer to parents/guardians and stay protected.
		{"Contact Mr. Paul Smith at home.", "Mr. Paul Smith"},
		{"Meeting with Mrs. Jane Doe scheduled.", "Mrs. Jane Doe"},
		// A name that merely starts with the letters "Dr" is not a title.
		{"Classmate Drew Smith attended.", "Drew Smith"},
	} {
		s := strings.Index(tc.text, tc.span)
		e := pfilter.Entity{Start: s, End: s + len(tc.span), Label: "private_person", Score: 1}
		got, applied := r.Apply(tc.text, ents(e), Policy{})
		if got == tc.text || len(applied) != 1 {
			t.Errorf("person should have been redacted in %q (got %q)", tc.text, got)
		}
	}
}

func TestPolicyValidate(t *testing.T) {
	if err := (Policy{Default: "bogus"}).Validate(); err == nil {
		t.Fatal("expected error for bad default mode")
	}
	if err := (Policy{ByLabel: map[string]Mode{"private_email": "nope"}}).Validate(); err == nil {
		t.Fatal("expected error for bad byLabel mode")
	}
	if err := (Policy{Default: ModeTag, ByLabel: map[string]Mode{"private_person": ModeKeepFirst}}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
