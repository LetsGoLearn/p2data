# Multi-stage build: compile the C++ engine + cgo Go binary, then ship a slim
# runtime. The model is NOT baked in — mount it at runtime (see README) to keep
# the image small and avoid embedding large weights in the image layers.

# ---- build stage ----
FROM golang:1.26-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
        cmake git build-essential ca-certificates libopenblas-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY . .

# Fetch submodules if the build context didn't include them.
RUN git submodule update --init --recursive third_party/privacy-filter.cpp || true

# Build static libpf.a + ggml (BLAS-accelerated CPU backend via OpenBLAS).
RUN cd third_party/privacy-filter.cpp \
    && cmake --preset release --fresh \
        -DBUILD_SHARED_LIBS=OFF -DGGML_METAL=OFF -DGGML_OPENMP=OFF \
        -DGGML_BLAS=ON -DGGML_BLAS_VENDOR=OpenBLAS \
    && cmake --build build/release --target pf -j

# Build the statically-linked redactor binary.
RUN CGO_ENABLED=1 go build -trimpath -o /out/redactor ./cmd/redactor

# ---- runtime stage ----
FROM debian:bookworm-slim
# libopenblas0: the engine dynamically links OpenBLAS (GGML_BLAS=ON build).
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libopenblas0 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 redactor
COPY --from=build /out/redactor /usr/local/bin/redactor

USER 10001
EXPOSE 8080
# Provide REDACTOR_API_KEYS and mount the model, e.g.:
#   docker run -p 8080:8080 \
#     -e REDACTOR_API_KEYS=... \
#     -e REDACTOR_MODEL_PATH=/models/privacy-filter-q8.gguf \
#     -v $PWD/models:/models:ro <image>
ENTRYPOINT ["/usr/local/bin/redactor"]
