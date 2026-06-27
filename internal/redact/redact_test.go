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
