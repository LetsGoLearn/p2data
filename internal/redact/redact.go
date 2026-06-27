package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/letsgolearn/p2data/internal/pfilter"
)

// lastTag is the placeholder substituted for a stripped surname in ModeKeepFirst.
const lastTag = "[LAST]"

// defaultTags maps the base-model raw labels to friendly tag names used by
// ModeTag and ModeHash.
var defaultTags = map[string]string{
	"private_email":   "EMAIL",
	"private_person":  "PERSON",
	"private_phone":   "PHONE",
	"private_address": "ADDRESS",
	"private_date":    "DATE",
	"private_url":     "URL",
	"account_number":  "ACCOUNT",
	"secret":          "SECRET",
}

// Redactor rewrites text given detected entities and a policy. It is immutable
// after construction and safe for concurrent use.
type Redactor struct {
	tags       map[string]string
	maskString string
	hashSecret []byte
}

// New builds a Redactor. maskString is used by ModeMask (default "[REDACTED]").
// hashSecret keys the HMAC used by ModeHash; if empty, ModeHash still works but
// hashes are not secret-keyed.
func New(maskString string, hashSecret []byte) *Redactor {
	if maskString == "" {
		maskString = "[REDACTED]"
	}
	return &Redactor{tags: defaultTags, maskString: maskString, hashSecret: hashSecret}
}

// Apply rewrites text according to p, using ents (byte-offset spans). It returns
// the redacted text and the entities that were actually redacted (sorted by
// start). Overlapping and out-of-range spans are skipped defensively. The
// original text of a span is never returned to the caller.
func (r *Redactor) Apply(text string, ents []pfilter.Entity, p Policy) (string, []pfilter.Entity) {
	sorted := append([]pfilter.Entity(nil), ents...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].End > sorted[j].End // longer span wins on a tie
	})

	var b strings.Builder
	applied := make([]pfilter.Entity, 0, len(sorted))
	cursor := 0
	for _, e := range sorted {
		if e.Start < cursor || e.Start < 0 || e.End > len(text) || e.Start >= e.End {
			continue // overlap or out-of-range
		}
		mode, ok := p.modeFor(e.Label)
		if !ok {
			continue // allow-list excluded this label; leave it in place
		}
		b.WriteString(text[cursor:e.Start])
		b.WriteString(r.replacement(text[e.Start:e.End], e.Label, mode))
		cursor = e.End
		applied = append(applied, e)
	}
	b.WriteString(text[cursor:])
	return b.String(), applied
}

// replacement computes the substitution for a single span. value is the
// original span text; it is consumed here for hashing/first-name handling and
// never leaves this package.
func (r *Redactor) replacement(value, label string, mode Mode) string {
	switch mode {
	case ModeDrop:
		return ""
	case ModeMask:
		return r.maskString
	case ModeHash:
		return r.tag(label) + "_" + r.hash(value)
	case ModeKeepFirst:
		return r.keepFirst(value)
	case ModeTag:
		fallthrough
	default:
		return "[" + r.tag(label) + "]"
	}
}

// keepFirst keeps the first whitespace-delimited token and replaces the rest
// with lastTag. A single-token value is returned unchanged (no surname to
// strip). This is a best-effort heuristic for Western "First Last" names.
func (r *Redactor) keepFirst(value string) string {
	fields := strings.Fields(value)
	if len(fields) <= 1 {
		return value
	}
	return fields[0] + " " + lastTag
}

// tag returns the friendly tag for a label, falling back to the upper-cased
// label without the "private_" prefix for unknown labels.
func (r *Redactor) tag(label string) string {
	if t, ok := r.tags[label]; ok {
		return t
	}
	return strings.ToUpper(strings.TrimPrefix(label, "private_"))
}

// hash returns the first 8 hex chars of HMAC-SHA256(value) under hashSecret.
func (r *Redactor) hash(value string) string {
	m := hmac.New(sha256.New, r.hashSecret)
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))[:8]
}
