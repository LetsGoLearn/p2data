package pfilter

import (
	"context"
	"strings"
	"testing"
)

// iepText mirrors the form-style layout of a real exported IEP document, the
// exact shape the NER model has been observed to miss.
const iepText = `STATE OF NEVADA

INDIVIDUALIZED EDUCATIONAL PROGRAM (IEP)

STUDENT/PARENT INFORMATION

Student

Diede, Anderson Cash

Birthdate 04/24/2015

Grade 03

Student ID #

20470

Student Primary Language

eng -English
`

// classify is a helper that fails the test on error.
func classify(t *testing.T, c Classifier, text string) []Entity {
	t.Helper()
	ents, err := c.Classify(context.Background(), text, 0.5)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	return ents
}

// span returns the entity covering exactly the first occurrence of want, or
// nil if no entity does.
func span(t *testing.T, text string, ents []Entity, want string) *Entity {
	t.Helper()
	start := strings.Index(text, want)
	if start < 0 {
		t.Fatalf("test text does not contain %q", want)
	}
	for i := range ents {
		if ents[i].Start == start && ents[i].End == start+len(want) {
			return &ents[i]
		}
	}
	return nil
}

func TestBackstop_IEPDocument(t *testing.T) {
	ents := classify(t, NewBackstop(), iepText)

	for _, tc := range []struct {
		value string
		label string
	}{
		{"Diede, Anderson Cash", "private_person"},
		{"04/24/2015", "private_date"},
		{"03", "private_grade"},
		{"20470", "account_number"},
	} {
		e := span(t, iepText, ents, tc.value)
		if e == nil {
			t.Errorf("%q not detected", tc.value)
			continue
		}
		if e.Label != tc.label {
			t.Errorf("%q labeled %q, want %q", tc.value, e.Label, tc.label)
		}
	}

	// Non-PII form content must not be flagged.
	for _, e := range ents {
		got := iepText[e.Start:e.End]
		for _, benign := range []string{"English", "NEVADA", "Language"} {
			if strings.Contains(got, benign) {
				t.Errorf("benign text %q flagged as %s", got, e.Label)
			}
		}
	}
}

func TestBackstop_DateFormats(t *testing.T) {
	for _, text := range []string{
		"DOB: 4-24-15",
		"born 2015-04-24",
		"born April 24, 2015",
		"born Apr. 24 2015",
		"born 24 April 2015",
	} {
		ents := classify(t, NewBackstop(), text)
		found := false
		for _, e := range ents {
			if e.Label == "private_date" {
				found = true
			}
		}
		if !found {
			t.Errorf("no private_date detected in %q", text)
		}
	}
}

func TestBackstop_LabeledFields(t *testing.T) {
	ents := classify(t, NewBackstop(), "Student Name: Doe, Jane\nEmployee ID: A-1042\nGrade: 5")
	labels := map[string]bool{}
	for _, e := range ents {
		labels[e.Label] = true
	}
	for _, want := range []string{"private_person", "account_number", "private_grade"} {
		if !labels[want] {
			t.Errorf("missing label %s in %v", want, ents)
		}
	}
}

func TestBackstop_NoFalsePositives(t *testing.T) {
	for _, text := range []string{
		"Thursday, July 23",       // calendar line, not "Last, First"
		"Meeting notes for today", // plain prose
		"The student will improve reading fluency by 20 percent.",
		"Reno is the biggest little city.",
	} {
		if ents := classify(t, NewBackstop(), text); len(ents) != 0 {
			t.Errorf("unexpected entities in %q: %v", text, ents)
		}
	}
}

func TestWithBackstop_MergesAndDedupes(t *testing.T) {
	// The Fake finds "Jane Doe"; the backstop finds the same DOB span the
	// fake cannot. Duplicate spans must not be emitted twice.
	c := WithBackstop(NewFake("Jane Doe"))
	defer c.Close()

	text := "Name: Jane Doe\nBirthdate 04/24/2015"
	ents := classify(t, c, text)

	seen := map[[2]int]int{}
	for _, e := range ents {
		seen[[2]int{e.Start, e.End}]++
	}
	for s, n := range seen {
		if n > 1 {
			t.Errorf("span %v emitted %d times", s, n)
		}
	}
	if e := span(t, text, ents, "04/24/2015"); e == nil {
		t.Error("merged classifier missed the date")
	}
	if e := span(t, text, ents, "Jane Doe"); e == nil {
		t.Error("merged classifier missed the fake's person match")
	}
}
