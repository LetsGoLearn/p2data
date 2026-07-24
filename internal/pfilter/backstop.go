package pfilter

import (
	"context"
	"regexp"
	"strings"
)

// Backstop is a deterministic, regex-based detector that supplements the NER
// model with structured-PII shapes the model reliably misses in form-style
// documents (IEPs, intake forms, exported PDFs), where labels and values are
// split across lines and names appear in "Last, First" order. It is not a
// replacement for the model: it only covers patterns that are cheap to match
// exactly, and it deliberately errs toward over-redaction.
//
// Matches are emitted with Score 1.0. Merge it with the model classifier via
// WithBackstop so no request can bypass it.
type Backstop struct{}

// NewBackstop returns the deterministic pattern detector.
func NewBackstop() *Backstop { return &Backstop{} }

// monthPat matches English month names and their common abbreviations.
const monthPat = `(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:t(?:ember)?)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)`

// namePart matches a single capitalized name token ("Diede", "O'Brien", "St.").
const namePart = `[A-Z][A-Za-z'’.-]{1,30}`

// backstopPattern is one regex rule. If group is 0 the whole match is the
// entity; otherwise the numbered capture group is.
type backstopPattern struct {
	label string
	re    *regexp.Regexp
	group int
}

var backstopPatterns = []backstopPattern{
	// Bare dates in common numeric formats: 04/24/2015, 4-24-15, 2015-04-24.
	{"private_date", regexp.MustCompile(`\b\d{1,2}[/-]\d{1,2}[/-]\d{2,4}\b`), 0},
	{"private_date", regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`), 0},
	// Written dates: "April 24, 2015", "Apr. 24 2015", "24 April 2015".
	{"private_date", regexp.MustCompile(`(?i)\b` + monthPat + `\.?\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4}\b`), 0},
	{"private_date", regexp.MustCompile(`(?i)\b\d{1,2}(?:st|nd|rd|th)?\s+` + monthPat + `\.?,?\s+\d{4}\b`), 0},

	// Labeled identifiers where the value may sit on a following line:
	// "Student ID # 20470", "Student ID #\n\n20470", "Employee ID: A-1042".
	{"account_number", regexp.MustCompile(`(?i)\b(?:student|pupil|employee|member|case|patient)[ \t]*id[ \t]*(?:#|no\.?|num(?:ber)?)?[ \t]*[:#]?\s{0,8}([A-Za-z]{0,4}-?\d[\dA-Za-z-]*)`), 1},

	// Grade level in education records (FERPA quasi-identifier):
	// "Grade 03", "Grade: 5", "Grade Level\n\nK".
	{"private_grade", regexp.MustCompile(`(?im)^[ \t]*grade(?:[ \t]+level)?[ \t]*[:#]?\s{0,8}(PK|K|\d{1,2})\b`), 1},

	// A "Last, First [Middle]" name occupying its own line, the standard
	// layout of student/parent fields in exported forms.
	{"private_person", regexp.MustCompile(`(?m)^[ \t]*(` + namePart + `,[ \t]+` + namePart + `(?:[ \t]+` + namePart + `){0,2})[ \t]*$`), 1},

	// A name (either order) on the line after a person field label:
	// "Student\n\nDiede, Anderson Cash" or "Parent:\nJane Doe".
	{"private_person", regexp.MustCompile(`(?m)^[ \t]*(?:Student|Pupil|Child|Parent|Guardian|Teacher|Case Manager)(?:[ \t]+Name)?[ \t]*:?[ \t]*\n\s{0,8}(` + namePart + `(?:,?[ \t]+` + namePart + `){1,3})[ \t]*$`), 1},

	// Inline "Name: John Smith" / "Student Name: Doe, Jane".
	{"private_person", regexp.MustCompile(`(?m)^[ \t]*(?:(?:Student|Pupil|Child|Parent|Guardian|Teacher)[ \t]+)?Name[ \t]*[:#][ \t]*(` + namePart + `(?:,?[ \t]+` + namePart + `){1,3})[ \t]*$`), 1},
}

// calendarWords guards the standalone "Last, First" rule against date-like
// lines such as "Thursday, July 23".
var calendarWords = map[string]bool{
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true,
	"september": true, "october": true, "november": true, "december": true,
}

// Classify returns all pattern matches. Matches are deterministic and emitted
// with Score 1.0, so the threshold only filters when callers pass a value > 1
// (mirroring Fake).
func (b *Backstop) Classify(_ context.Context, text string, threshold float32) ([]Entity, error) {
	if threshold > 1.0 {
		return nil, nil
	}
	seen := map[[2]int]bool{}
	var out []Entity
	for _, p := range backstopPatterns {
		for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := m[2*p.group], m[2*p.group+1]
			if start < 0 || start >= end {
				continue
			}
			if p.label == "private_person" && looksLikeDate(text[start:end]) {
				continue
			}
			span := [2]int{start, end}
			if seen[span] {
				continue
			}
			seen[span] = true
			out = append(out, Entity{Start: start, End: end, Score: 1.0, Label: p.label})
		}
	}
	return out, nil
}

// Close is a no-op; Backstop holds no native resources.
func (b *Backstop) Close() error { return nil }

// looksLikeDate reports whether a candidate person span starts with a weekday
// or month name (e.g. "Thursday, July").
func looksLikeDate(s string) bool {
	first, _, _ := strings.Cut(s, ",")
	return calendarWords[strings.ToLower(strings.TrimSpace(first))]
}

// merged runs a primary (model) classifier and the Backstop, returning the
// union of their entities. Overlapping spans are resolved downstream by
// redact.Apply (earliest/longest span wins).
type merged struct {
	primary  Classifier
	backstop *Backstop
}

// WithBackstop wraps c so every Classify call also includes the deterministic
// Backstop matches. Closing the returned classifier closes c.
func WithBackstop(c Classifier) Classifier {
	return &merged{primary: c, backstop: NewBackstop()}
}

func (m *merged) Classify(ctx context.Context, text string, threshold float32) ([]Entity, error) {
	ents, err := m.primary.Classify(ctx, text, threshold)
	if err != nil {
		return nil, err
	}
	extra, err := m.backstop.Classify(ctx, text, threshold)
	if err != nil {
		return nil, err
	}
	// Drop backstop spans identical to a model span so callers don't see
	// duplicate entities; partial overlaps are left for redact.Apply.
	seen := make(map[[2]int]bool, len(ents))
	for _, e := range ents {
		seen[[2]int{e.Start, e.End}] = true
	}
	for _, e := range extra {
		if !seen[[2]int{e.Start, e.End}] {
			ents = append(ents, e)
		}
	}
	return ents, nil
}

func (m *merged) Close() error { return m.primary.Close() }
