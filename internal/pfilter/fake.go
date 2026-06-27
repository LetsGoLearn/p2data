package pfilter

import (
	"context"
	"regexp"
)

// Fake is a regex-based Classifier for tests and local development. It detects a
// few PII shapes without the native engine or a model file, so the HTTP and
// redaction layers can be exercised with CGO_ENABLED=0. It is NOT a real NER
// model and must not be used in production.
type Fake struct {
	patterns []fakePattern
}

type fakePattern struct {
	label string
	re    *regexp.Regexp
}

// NewFake returns a Fake that recognizes emails, phone numbers, and any name in
// names (matched as whole words) as private_person.
func NewFake(names ...string) *Fake {
	patterns := []fakePattern{
		{"private_email", regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
		{"private_phone", regexp.MustCompile(`\b(?:\+?\d[\d\-.\s]{6,}\d)\b`)},
	}
	for _, n := range names {
		patterns = append(patterns, fakePattern{
			"private_person", regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`),
		})
	}
	return &Fake{patterns: patterns}
}

// Classify returns matches from every configured pattern. The score is fixed at
// 1.0, so the threshold only filters when callers pass a value > 1.
func (f *Fake) Classify(_ context.Context, text string, threshold float32) ([]Entity, error) {
	var out []Entity
	for _, p := range f.patterns {
		for _, m := range p.re.FindAllStringIndex(text, -1) {
			if threshold > 1.0 {
				continue
			}
			out = append(out, Entity{Start: m[0], End: m[1], Score: 1.0, Label: p.label})
		}
	}
	return out, nil
}

// Close is a no-op for the fake.
func (f *Fake) Close() error { return nil }
