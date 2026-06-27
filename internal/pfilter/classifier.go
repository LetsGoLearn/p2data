// Package pfilter is the seam between the application and the
// privacy-filter.cpp NER engine. Everything outside this package depends only
// on the Classifier interface and the Entity type, so the HTTP and redaction
// layers can be built and tested with CGO_ENABLED=0 using the in-memory fake.
//
// The real engine is bound via cgo in cgo.go (build tag: cgo). When the binary
// is built without cgo, stub.go provides a Classifier that fails fast.
package pfilter

import "context"

// Entity is a PII span detected in the input text. Start and End are byte
// offsets into the original UTF-8 text (half-open: text[Start:End]), matching
// the offsets returned by the underlying pf_classify call. Label is the decoded
// entity group (e.g. "private_email"); Score is the model confidence in [0,1].
type Entity struct {
	Start int     `json:"start"`
	End   int     `json:"end"`
	Score float32 `json:"score"`
	Label string  `json:"label"`
}

// LoadOptions configures loading the native engine. PoolSize is the number of
// independent model contexts to load; each is a full copy of the model in
// memory and serves one request at a time, so PoolSize is the max concurrency.
type LoadOptions struct {
	ModelPath string
	Device    string // "cpu", "gpu", "cuda", "vulkan" (optionally ":N")
	NThreads  int    // <= 0 picks the engine default (CPU only)
	Window    int    // max tokens per forward pass; <= 0 keeps the default
	PoolSize  int    // number of contexts; < 1 is treated as 1
}

// Classifier detects PII spans in text. Implementations must be safe for
// concurrent use by multiple goroutines (the cgo implementation guarantees this
// with a pool of single-threaded contexts).
type Classifier interface {
	// Classify returns the PII spans in text whose score is >= threshold.
	// The returned spans are byte offsets into text and need not be sorted.
	Classify(ctx context.Context, text string, threshold float32) ([]Entity, error)
	// Close releases any native resources held by the classifier.
	Close() error
}
