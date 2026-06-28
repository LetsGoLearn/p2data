//go:build cgo

package pfilter

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/privacy-filter.cpp/include

// Static link against libpf.a and the bundled ggml static libs. The -L paths
// match the `release` cmake preset built with -DBUILD_SHARED_LIBS=OFF
// -DGGML_METAL=OFF -DGGML_OPENMP=OFF -DGGML_BLAS=ON (BLAS-accelerated CPU
// backend: Apple Accelerate on macOS, OpenBLAS on Linux). See the Makefile
// `lib` target.
//
// The ggml BLAS backend is a SEPARATE static lib (libggml-blas.a) that
// self-registers via a global constructor. The linker would drop that
// unreferenced object from a normal -l, so it is force-loaded (whole archive),
// placed before -lggml-base, with the system BLAS linked after.
//
// For a GGML_BLAS=OFF build, remove the ggml-blas / -lopenblas flags below
// (and the ggml-blas -L); Accelerate may stay linked on macOS.
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/privacy-filter.cpp/build/release
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/privacy-filter.cpp/build/release/ggml/src
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/privacy-filter.cpp/build/release/ggml/src/ggml-blas
#cgo LDFLAGS: -lpf -lggml -lggml-cpu
#cgo linux LDFLAGS: -Wl,--whole-archive -lggml-blas -Wl,--no-whole-archive
#cgo darwin LDFLAGS: -Wl,-force_load,${SRCDIR}/../../third_party/privacy-filter.cpp/build/release/ggml/src/ggml-blas/libggml-blas.a
#cgo LDFLAGS: -lggml-base
#cgo darwin LDFLAGS: -lc++ -framework Accelerate -framework Foundation
#cgo linux LDFLAGS: -lstdc++ -lm -lpthread -ldl -lopenblas

#include <stdlib.h>
#include "pf.h"
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"
)

// engineCtx wraps one native pf_ctx. A single pf_ctx is not safe for concurrent
// pf_classify calls, so each is owned by exactly one in-flight request at a time
// (enforced by the pool's channel).
type engineCtx struct {
	ptr *C.pf_ctx
}

func loadEngineCtx(opts LoadOptions) (*engineCtx, error) {
	cPath := C.CString(opts.ModelPath)
	defer C.free(unsafe.Pointer(cPath))

	var cDevice *C.char
	if opts.Device != "" {
		cDevice = C.CString(opts.Device)
		defer C.free(unsafe.Pointer(cDevice))
	}

	ptr := C.pf_load(cPath, cDevice, C.int(opts.NThreads))
	if ptr == nil {
		// pf_last_error(NULL) is documented NULL-safe and returns the load error.
		return nil, fmt.Errorf("pf_load failed: %s", C.GoString(C.pf_last_error(nil)))
	}
	if opts.Window > 0 {
		C.pf_set_window(ptr, C.int32_t(opts.Window))
	}
	return &engineCtx{ptr: ptr}, nil
}

func (e *engineCtx) classify(text string, threshold float32) ([]Entity, error) {
	var cText *C.char
	var cLen C.size_t
	if len(text) > 0 {
		cText = (*C.char)(unsafe.Pointer(unsafe.StringData(text)))
		cLen = C.size_t(len(text))
	}

	var out *C.pf_entity
	var n C.size_t
	rc := C.pf_classify(e.ptr, cText, cLen, C.float(threshold), &out, &n)
	if rc != 0 {
		return nil, fmt.Errorf("pf_classify failed: %s", C.GoString(C.pf_last_error(e.ptr)))
	}
	defer C.pf_entities_free(out, n)

	count := int(n)
	if count == 0 {
		return nil, nil
	}
	ents := make([]Entity, count)
	span := unsafe.Slice(out, count)
	for i := 0; i < count; i++ {
		ents[i] = Entity{
			Start: int(span[i].start),
			End:   int(span[i].end),
			Score: float32(span[i].score),
			Label: C.GoString(span[i].label),
		}
	}
	return ents, nil
}

func (e *engineCtx) free() {
	if e.ptr != nil {
		C.pf_free(e.ptr)
		e.ptr = nil
	}
}

// pool is a Classifier backed by a fixed set of engine contexts. Each Classify
// checks out one context, so at most len(contexts) classifications run at once.
type pool struct {
	free     chan *engineCtx
	all      []*engineCtx
	closeOne sync.Once
}

// Load loads PoolSize copies of the model and returns a concurrency-safe
// Classifier. Each copy holds the full model in memory.
func Load(opts LoadOptions) (Classifier, error) {
	size := opts.PoolSize
	if size < 1 {
		size = 1
	}
	p := &pool{free: make(chan *engineCtx, size)}
	for i := 0; i < size; i++ {
		ctx, err := loadEngineCtx(opts)
		if err != nil {
			p.Close() // free any already-loaded contexts
			return nil, err
		}
		p.all = append(p.all, ctx)
		p.free <- ctx
	}
	return p, nil
}

func (p *pool) Classify(ctx context.Context, text string, threshold float32) ([]Entity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case e := <-p.free:
		defer func() { p.free <- e }()
		return e.classify(text, threshold)
	}
}

func (p *pool) Close() error {
	p.closeOne.Do(func() {
		for _, e := range p.all {
			e.free()
		}
	})
	return nil
}
