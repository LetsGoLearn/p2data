//go:build !cgo

package pfilter

import "errors"

// ErrNoCGO is returned by Load when the binary was built without cgo, i.e.
// without the native privacy-filter.cpp engine linked in.
var ErrNoCGO = errors.New("pfilter: built without cgo; native engine unavailable (use the Fake classifier or rebuild with CGO_ENABLED=1)")

// Load reports that the native engine is unavailable in a non-cgo build.
func Load(LoadOptions) (Classifier, error) {
	return nil, ErrNoCGO
}
