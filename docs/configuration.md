# Configuration reference

## node-scanner flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scan-interval` | `60s` | How often to re-scan for block devices |
| `--mount-base` | `/mnt/cubbit` | Base directory for mounts (`<base>/<uuid>`) |
| `--fs-type` | `ext4` | Filesystem type to look for and format with |
| `--min-size` | `52428800` | Minimum device size in bytes (50 MiB). The production DaemonSet sets this to `16106127360` (15 GiB) |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |

`NODE_NAME` must be injected via the downward API (already configured in `deploy/daemonset.yaml`).

## provisioner flags

| Flag | Default | Description |
|------|---------|-------------|
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |

## webhook flags

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8443` | HTTPS listen address |
| `--self-sign` | `false` | Generate self-signed TLS cert, store in a Secret, and patch the `MutatingWebhookConfiguration` |
| `--tls-cert` | `/certs/tls.crt` | TLS certificate file (used when `--self-sign` is not set) |
| `--tls-key` | `/certs/tls.key` | TLS private key file (used when `--self-sign` is not set) |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |

## Node requirements

The `node-scanner` DaemonSet runs privileged and relies on the following being present on every worker node:

| Tool | Purpose | Package |
|------|---------|---------|
| `lsblk` | Enumerate block devices, UUIDs, sizes, and filesystem types | `util-linux` |
| `udevadm` | Resolve device paths and trigger udev after format | `systemd-udev` / `udev` |
| `mkfs.ext4` (or matching `mkfs.<fstype>`) | Format raw disks | `e2fsprogs` for ext4; `xfsprogs` for xfs |
| `mount` | Mount formatted disks | `util-linux` |

The DaemonSet mounts `/run/udev` from the host (read-only) to access the udev database without needing `udevadm` inside the container. **The host must be running `systemd-udevd` or equivalent** for `/run/udev` to exist.

> **Minimal images (Alpine, Talos):** verify `lsblk` and a udev daemon are present. On Alpine, install `util-linux` and `eudev`. On Talos, udev is built in.

## Using cert-manager instead of --self-sign

1. Create a `Certificate` resource:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: local-disk-webhook
  namespace: kube-system
spec:
  secretName: local-disk-webhook-tls
  issuerRef:
    name: cluster-issuer   # your issuer
    kind: ClusterIssuer
  dnsNames:
    - local-disk-webhook.kube-system.svc
    - local-disk-webhook.kube-system.svc.cluster.local
```

2. Remove `--self-sign` from the webhook Deployment args and add volume mounts for the Secret.

3. Annotate the `MutatingWebhookConfiguration` so cert-manager injects the `caBundle` automatically:

```yaml
metadata:
  annotations:
    cert-manager.io/inject-ca-from: kube-system/local-disk-webhook
```

## Building

```bash
# Build all binaries
make build

# Build Docker images (override registry and version as needed)
make docker-build REGISTRY=your.registry.io VERSION=v1.0.0

# Push images
make docker-push REGISTRY=your.registry.io VERSION=v1.0.0

# Run tests
make test

# Lint (requires golangci-lint)
make lint
```

## Development

```bash
go mod download
go test ./internal/... -v -race
```

`internal/scanner` has no unit tests — its logic requires real Linux block devices. Test it on a VM with loop devices (`losetup`).

The provisioner and webhook tests use `fake.NewSimpleClientset()` from `k8s.io/client-go` and do not require a running cluster.
