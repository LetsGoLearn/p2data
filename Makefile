# Secure PII-redaction API over privacy-filter.cpp (cgo).

PF_DIR    := third_party/privacy-filter.cpp
BUILD_DIR := $(PF_DIR)/build/release
MODEL     ?= q8
BIN       := bin/redactor

.PHONY: all submodules lib model build run test test-cgo vet clean deploy-lxc

all: lib build

## Fetch the privacy-filter.cpp submodule (and its ggml submodule).
submodules:
	git submodule update --init --recursive $(PF_DIR)

## Build libpf.a + ggml static libs (CPU + BLAS, no Metal). Requires cmake.
lib:
	cd $(PF_DIR) && cmake --preset release --fresh \
		-DBUILD_SHARED_LIBS=OFF -DGGML_METAL=OFF -DGGML_BLAS=OFF -DGGML_OPENMP=OFF
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

clean:
	rm -rf bin $(BUILD_DIR)
