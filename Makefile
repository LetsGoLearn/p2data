# Secure PII-redaction API over privacy-filter.cpp (cgo).

PF_DIR    := third_party/privacy-filter.cpp
BUILD_DIR := $(PF_DIR)/build/release
MODEL     ?= q8
BIN       := bin/redactor

# BLAS acceleration for the engine's GEMM-heavy NER forward pass — a large CPU
# speedup. Vendor defaults per-OS: Apple Accelerate on macOS, OpenBLAS on Linux
# (needs libopenblas-dev). Set GGML_BLAS=OFF for a portable, slower build with
# no BLAS dependency — but then also drop the ggml-blas link flags in
# internal/pfilter/cgo.go, which assume the BLAS backend is present.
GGML_BLAS ?= ON
ifeq ($(shell uname -s),Darwin)
  GGML_BLAS_VENDOR ?= Apple
else
  GGML_BLAS_VENDOR ?= OpenBLAS
endif

# cgo rejects -Wl,--whole-archive / -force_load in #cgo LDFLAGS by default
# (https://go.dev/s/invalidflag). The ggml-blas backend is a self-registering
# static archive that must be force-loaded (see internal/pfilter/cgo.go), so
# allow those flags for every cgo build/vet below.
export CGO_LDFLAGS_ALLOW := -Wl,--whole-archive|-Wl,--no-whole-archive|-Wl,-force_load.*

.PHONY: all submodules lib model build run test test-cgo vet clean deploy-lxc update-lxc

all: lib build

## Fetch the privacy-filter.cpp submodule (and its ggml submodule).
submodules:
	git submodule update --init --recursive $(PF_DIR)

## Build libpf.a + ggml static libs (CPU, BLAS-accelerated, no Metal). Requires cmake.
lib:
	cd $(PF_DIR) && cmake --preset release --fresh \
		-DBUILD_SHARED_LIBS=OFF -DGGML_METAL=OFF -DGGML_OPENMP=OFF \
		-DGGML_BLAS=$(GGML_BLAS) -DGGML_BLAS_VENDOR=$(GGML_BLAS_VENDOR)
	cmake --build $(BUILD_DIR) --target pf -j

## Download a GGUF model into ./models (MODEL=q8|f16).
model:
	bash scripts/download-model.sh $(MODEL)

## Build the redactor binary (cgo, statically linked against libpf.a).
build:
	CGO_ENABLED=1 go build -o $(BIN) ./cmd/redactor

## Run the pure-Go test suites (no cgo, no model needed).
test:
	CGO_ENABLED=0 go test ./internal/redact/... ./internal/api/... ./internal/config/...

## Build everything with cgo and vet (requires lib built).
test-cgo:
	CGO_ENABLED=1 go vet ./...

vet:
	CGO_ENABLED=0 go vet ./...

## Run the server (set REDACTOR_API_KEYS and REDACTOR_MODEL_PATH first).
run: build
	./$(BIN)

## Create a Proxmox LXC and deploy into it. Run on the Proxmox host.
## Override settings via env, e.g. CTID=210 MEMORY=6144 make deploy-lxc
deploy-lxc:
	bash deploy/proxmox/create-lxc.sh

## Update an already-deployed LXC in place (push source, rebuild, restart).
## Run on the Proxmox host. Override via env, e.g. CTID=210 make update-lxc
update-lxc:
	bash deploy/proxmox/update-lxc.sh

clean:
	rm -rf bin $(BUILD_DIR)
