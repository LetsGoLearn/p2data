// Package redact turns detected PII spans into redacted text according to a
// per-label policy. It is pure Go and has no dependency on the native engine,
// so it is fully unit-testable with CGO_ENABLED=0.
package redact

import "fmt"

// Mode is how a single detected span is rewritten.
type Mode string

const (
	// ModeTag replaces the span with its bracketed type, e.g. "[EMAIL]".
	ModeTag Mode = "tag"
	// ModeMask replaces the span with a fixed mask string, hiding the type.
	ModeMask Mode = "mask"
	// ModeHash replaces the span with TYPE_<hmac8>, stable for equal values.
	ModeHash Mode = "hash"
	// ModeKeepFirst keeps the first whitespace token (first name) and strips
	// the remainder, e.g. "Jane Doe" -> "Jane [LAST]". Intended for persons.
	ModeKeepFirst Mode = "keepFirst"
	// ModeDrop removes the span entirely.
	ModeDrop Mode = "drop"
)

// Valid reports whether m is a recognized mode.
func (m Mode) Valid() bool {
	switch m {
	case ModeTag, ModeMask, ModeHash, ModeKeepFirst, ModeDrop:
		return true
	default:
		return false
	}
}

// Policy controls which labels are redacted and how. The zero value redacts
// every detected label with ModeTag.
type Policy struct {
	// Default is the mode for any label without a ByLabel override. Empty means
	// ModeTag.
	Default Mode `json:"default,omitempty"`
	// ByLabel overrides the mode for specific raw labels (e.g. "private_person").
	ByLabel map[string]Mode `json:"byLabel,omitempty"`
	// Labels, when non-empty, is an allow-list: only these raw labels are
	// redacted and all others pass through untouched. Empty means "all labels".
	Labels []string `json:"labels,omitempty"`
}

// Validate checks every mode in the policy is recognized.
func (p Policy) Validate() error {
	if p.Default != "" && !p.Default.Valid() {
		return fmt.Errorf("invalid default mode %q", p.Default)
	}
	for label, m := range p.ByLabel {
		if !m.Valid() {
			return fmt.Errorf("invalid mode %q for label %q", m, label)
		}
	}
	return nil
}

// modeFor resolves the mode for a label and whether it should be redacted at
// all (false means leave the span untouched).
func (p Policy) modeFor(label string) (Mode, bool) {
	if len(p.Labels) > 0 {
		found := false
		for _, l := range p.Labels {
			if l == label {
				found = true
				break
			}
		}
		if !found {
			return "", false
		}
	}
	if m, ok := p.ByLabel[label]; ok {
		return m, true
	}
	if p.Default != "" {
		return p.Default, true
	}
	return ModeTag, true
}
