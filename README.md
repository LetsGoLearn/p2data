# redactor — secure PII-redaction API

A small, secure Go HTTP API that redacts PII from text using the
[`privacy-filter.cpp`](https://github.com/localai-org/privacy-filter.cpp)
NER engine (OpenAI's privacy-filter token-classification model), bound via
**cgo** and statically linked into a single binary.

## Features

- **Real NER redaction** — detects PII spans with exact UTF-8 byte offsets via
  the engine's flat C API (`pf_classify`).
- **Per-label redaction policy** — choose how each PII type is rewritten:
  - `tag` (default) → `[EMAIL]`, `[PHONE]`, …
  - `mask` → a fixed string (`[REDACTED]`)
  - `hash` → stable keyed token, e.g. `EMAIL_a1b2c3d4` (same value → same token)
  - `keepFirst` → keep the first name, strip the surname: `Jane Doe` → `Jane [LAST]`
  - `drop` → remove the span entirely
- **Secure by default** — API-key auth (constant-time compare), no-PII logging
  (only counts/labels/latency are logged), request body-size cap, timeouts, and
  panic recovery.
- **Testable without the native engine** — the HTTP and redaction layers depend
  only on a `Classifier` interface, so they build and test with `CGO_ENABLED=0`
  using an in-memory fake.

## Supported labels (base English model)

`private_person`, `private_email`, `private_phone`, `private_address`,
`private_date`, `private_url`, `account_number`, `secret`.

## Build & run

Requires Go 1.26+, a C/C++ toolchain, and `cmake`.

```sh
make submodules          # fetch privacy-filter.cpp + ggml
make lib                 # build static libpf.a + ggml (CPU, no Metal/BLAS)
make model MODEL=q8      # download the GGUF into ./models (q8 ~1.5GB, f16 ~2.8GB)
make build               # build the cgo binary -> bin/redactor

export REDACTOR_API_KEYS="$(openssl rand -hex 24)"
export REDACTOR_MODEL_PATH="$PWD/models/privacy-filter-q8.gguf"
./bin/redactor
```

Local development without the native engine or a model:

```sh
REDACTOR_USE_FAKE=1 REDACTOR_API_KEYS=dev CGO_ENABLED=0 go run ./cmd/redactor
```

## API

All `/v1/*` endpoints require `Authorization: Bearer <key>` or `X-API-Key: <key>`.
`/healthz` and `/readyz` are open.

### `POST /v1/redact`

```jsonc
{
  "text": "Hi, I am Jane Doe. Email me at jane.doe@acme.com or call 415-555-0142.",
  "threshold": 0.5,                                  // optional, default REDACTOR_DEFAULT_THRESHOLD
  "policy": {
    "default": "tag",                                // optional, default "tag"
    "byLabel": { "private_person": "keepFirst" },    // optional per-label modes
    "labels":  ["private_person","private_email","private_phone"]  // optional allow-list
  }
}
```

Response:

```json
{
  "redacted": "Hi, I am Jane [LAST]. Email me at [EMAIL] or call [PHONE].",
  "entities": [
    {"start":9,"end":17,"score":0.99,"label":"private_person"},
    {"start":31,"end":48,"score":0.99,"label":"private_email"},
    {"start":57,"end":69,"score":0.99,"label":"private_phone"}
  ],
  "counts": {"private_person":1,"private_email":1,"private_phone":1}
}
```

The response never echoes the original PII values — only offsets, labels, and
scores.

### `GET /v1/labels`

Lists the supported labels and their friendly tags.

## Configuration (environment)

| Variable | Default | Purpose |
|---|---|---|
| `REDACTOR_API_KEYS` | *(required)* | Comma-separated API keys |
| `REDACTOR_MODEL_PATH` | *(required)* | Path to the GGUF model |
| `REDACTOR_LISTEN_ADDR` | `:8080` | Listen address |
| `REDACTOR_DEVICE` | `cpu` | `cpu`/`gpu`/`cuda`/`vulkan` (engine build dependent) |
| `REDACTOR_N_THREADS` | `0` | CPU threads (`0` = engine default) |
| `REDACTOR_WINDOW` | `0` | Max tokens per forward pass (`0` = default 4096) |
| `REDACTOR_POOL_SIZE` | `1` | Model contexts = max concurrency (each is a full model copy in RAM) |
| `REDACTOR_DEFAULT_THRESHOLD` | `0.5` | Confidence cutoff |
| `REDACTOR_MASK_STRING` | `[REDACTED]` | String used by `mask` mode |
| `REDACTOR_HASH_SECRET` | *(empty)* | HMAC key for `hash` mode |
| `REDACTOR_MAX_BODY_BYTES` | `1048576` | Request body cap (413 over) |
| `REDACTOR_READ_TIMEOUT` / `WRITE_TIMEOUT` / `IDLE_TIMEOUT` | `15s` / `30s` / `60s` | Server timeouts |
| `REDACTOR_USE_FAKE` | *(unset)* | `1` uses the regex fake (dev only) |

## Testing

```sh
make test          # pure-Go suites (no cgo, no model)
make test-cgo      # vet the cgo build (requires `make lib`)
```

## Security notes

- TLS is expected to terminate at a reverse proxy / load balancer. In-app TLS
  can be added to `cmd/redactor` if needed.
- `POOL_SIZE` multiplies memory by the full model size; raise only if you have
  the RAM and need more concurrency.
- The `keepFirst` person mode is a whitespace heuristic ("First Last"); it won't
  perfectly handle every name format (e.g. "Doe, Jane").

## Deploy to a Proxmox LXC

A turn-key deploy into an unprivileged Debian 12 LXC (builds in-container,
downloads the model, installs a hardened systemd service) lives in
[`deploy/proxmox/`](deploy/proxmox/README.md). From the Proxmox host:

```sh
CTID=210 IP=192.168.1.50/24 GATEWAY=192.168.1.1 make deploy-lxc
```

## Docker

```sh
docker build -t redactor .
docker run -p 8080:8080 \
  -e REDACTOR_API_KEYS=... \
  -e REDACTOR_MODEL_PATH=/models/privacy-filter-q8.gguf \
  -v "$PWD/models:/models:ro" redactor
```
