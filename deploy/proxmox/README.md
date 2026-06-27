# Deploying redactor to a Proxmox LXC

This deploys the redactor service into an **unprivileged Debian 12 LXC**. The
binary is built **inside the container** (Go + cmake + ggml), the model is
downloaded into the container, and the service runs under a hardened systemd
unit. TLS is expected to terminate upstream (reverse proxy / load balancer).

## Files

| File | Runs on | Purpose |
|---|---|---|
| `create-lxc.sh` | Proxmox host | Creates the CT, pushes source, runs `provision.sh` |
| `provision.sh` | Inside the CT | Installs toolchain, builds, downloads model, installs the service |
| `redactor.service` | Inside the CT | Hardened systemd unit |
| `redactor.env.example` | Inside the CT | Env template (key + model path filled in by `provision.sh`) |

## One-shot deploy (from the Proxmox host)

Copy this repo to the Proxmox host (or clone it there), then:

```sh
# defaults: CTID=200, 4 cores, 4096 MiB, 16 GiB disk, DHCP, q8 model
bash deploy/proxmox/create-lxc.sh

# or customize:
CTID=210 CORES=6 MEMORY=6144 DISK=20 \
  IP=192.168.1.50/24 GATEWAY=192.168.1.1 \
  MODEL_VARIANT=q8 STORAGE=local-lvm BRIDGE=vmbr0 \
  bash deploy/proxmox/create-lxc.sh
```

The script prints the generated API key and the container IP at the end.

### Sizing

- **Memory**: the q8 model is ~1.5 GB resident; f16 ~2.8 GB. Each
  `REDACTOR_POOL_SIZE` context is a full copy. 4 GiB is fine for pool size 1
  (q8); bump memory before raising the pool.
- **Disk**: the build (Go + ggml objects) plus the model needs headroom; 16 GiB
  default.
- **CPU**: more cores = faster inference (the engine threads internally).

### Template

`create-lxc.sh` defaults to `local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst`.
If you don't have it:

```sh
pveam update
pveam available | grep debian-12-standard
pveam download local debian-12-standard_12.7-1_amd64.tar.zst
```

## Deploy into an existing container

If the CT already exists, copy the repo into it and run the provisioner:

```sh
# from the host
pct push <CTID> redactor-src.tgz /tmp/src.tgz   # or rsync/scp the tree
pct exec <CTID> -- bash -c 'mkdir -p /opt/redactor/src && tar xzf /tmp/src.tgz -C /opt/redactor/src'
pct exec <CTID> -- env APP_SRC=/opt/redactor/src MODEL_VARIANT=q8 \
  bash /opt/redactor/src/deploy/proxmox/provision.sh
```

## Operating the service

```sh
pct exec <CTID> -- systemctl status redactor
pct exec <CTID> -- journalctl -u redactor -f      # metadata-only logs, no PII
pct exec <CTID> -- systemctl restart redactor

# config (API keys, model path, threading, pool size)
pct exec <CTID> -- nano /etc/redactor/redactor.env
pct exec <CTID> -- systemctl restart redactor
```

Smoke test (replace KEY and IP):

```sh
curl -fsS http://<CT-IP>:8080/readyz
curl -fsS -H "Authorization: Bearer <KEY>" \
  -H 'Content-Type: application/json' \
  -d '{"text":"Email Jane Doe at jane@acme.com","policy":{"byLabel":{"private_person":"keepFirst"}}}' \
  http://<CT-IP>:8080/v1/redact
```

## Upgrading

Re-run `provision.sh` after updating the source (it is idempotent — it keeps the
existing `/etc/redactor/redactor.env` and the downloaded model, rebuilds the
binary, and restarts the service):

```sh
# push the new source over /opt/redactor/src, then:
pct exec <CTID> -- env APP_SRC=/opt/redactor/src bash /opt/redactor/src/deploy/proxmox/provision.sh
```

## Notes

- The container is created **unprivileged** with `nesting=1`. The build needs no
  special privileges; the running service is sandboxed by the systemd unit
  (`ProtectSystem=strict`, `NoNewPrivileges`, syscall filter, etc.).
- If the service fails to start with a syscall/sandbox error on an older kernel,
  loosen `redactor.service` (e.g. remove `SystemCallFilter`) and re-run
  `systemctl daemon-reload && systemctl restart redactor`.
