# kubernetes-external-provider

A custom Kubernetes external provisioner that provisions local persistent volumes based on disk partition UUIDs. Simpler than a full CSI driver — inspired by [local-path-provisioner](https://github.com/rancher/local-path-provisioner) but designed for workloads that need to claim a specific physical disk across reboots without hardcoding node names.

Built with [`sig-storage-lib-external-provisioner/v13`](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner) and the disk management library from [`cubbitgg/cmd-drivers`](https://github.com/cubbitgg/cmd-drivers).

## Architecture

```
Worker Node                            Control Plane
┌──────────────────────────┐          ┌──────────────────────────────────────┐
│  NodeScanner DaemonSet   │          │                                      │
│  ┌────────────────────┐  │          │  ┌──────────────────────────────┐    │
│  │  driver-init       │  │          │  │  Provisioner Deployment      │    │
│  │  driver-mounter    │──┼────────▶│  │  (external-provisioner)      │    │
│  │  node-scanner      │  │ labels/  │  └──────────────────────────────┘    │
│  └────────────────────┘  │ annots   │                                      │
│                          │          │  ┌──────────────────────────────┐    │
│  Disks                   │          │  │  Scheduler Extender          │    │
│  /dev/sdb (uuid=abc...)  │          │  │  (POST /filter)              │    │
│  /dev/sdc (uuid=def...)  │          │  └──────────────────────────────┘    │
└──────────────────────────┘          │                ▲                     │
                                      │  ┌─────────────┴────────────────┐    │
                                      │  │  kube-scheduler              │    │
                                      │  └──────────────────────────────┘    │
                                      └──────────────────────────────────────┘
```

### Components

| Component | Kind | Purpose |
|-----------|------|---------|
| `node-scanner` | DaemonSet | Discovers disks, mounts them under `/mnt/cubbit/<uuid>`, labels/annotates nodes |
| `provisioner` | Deployment | Creates PVs with correct `spec.local.path` and `nodeAffinity` |
| `scheduler-extender` | Deployment | Filters scheduling candidates to nodes that physically hold the requested disk |

### End-to-end flow

1. **NodeScanner** runs on every node, uses the `cmd-drivers` library to format raw disks, mount them, then patches the node:
   - Label: `agent.cubbit.io/has-uuid-<UUID>: "true"` for each mounted disk
   - Annotation: `agent.cubbit.io/disk-uuid-map: {"<uuid>": {"path": "/mnt/cubbit/<uuid>", "size": <bytes>}}`

2. **User creates a PVC** with annotation `agent.cubbit.io/disk-uuid: <UUID>` and `storageClassName: local-disk`.

3. **Scheduler** evaluates candidate nodes. The **Scheduler Extender** intercepts the filter step and removes any node that doesn't carry the label for the requested UUID.

4. Scheduler binds the pod to the matching node and sets `volume.kubernetes.io/selected-node` on the PVC.

5. **Provisioner** sees the bound PVC, reads the node's disk-map annotation, and creates a PV:
   - `spec.local.path` = mount path from the annotation
   - `spec.nodeAffinity` = pinned to the selected node
   - `reclaimPolicy: Retain` (disk data persists across PVC deletion)

6. Pod starts on the correct node with the correct disk mounted.

## Prerequisites

- Kubernetes ≥ 1.29 (uses `kubescheduler.config.k8s.io/v1`)
- Nodes with ext4-formatted block devices (or raw devices that `driver-init` will format)
- Access to modify the kube-scheduler configuration (see [Scheduler Setup](#scheduler-setup))

## Deployment

### 1. Apply all manifests

```bash
kubectl apply -f deploy/rbac.yaml
kubectl apply -f deploy/storageclass.yaml
kubectl apply -f deploy/daemonset.yaml
kubectl apply -f deploy/provisioner.yaml
kubectl apply -f deploy/scheduler-extender.yaml
```

### 2. Scheduler Setup

The scheduler extender must be registered with the kube-scheduler via a `KubeSchedulerConfiguration`. The config is in `deploy/scheduler-config.yaml`.

**For kubeadm clusters:**

```bash
# Copy the config to the control-plane node
scp deploy/scheduler-config.yaml <control-plane>:/etc/kubernetes/scheduler-config.yaml

# Edit the kube-scheduler static pod manifest
vi /etc/kubernetes/manifests/kube-scheduler.yaml
# Add: --config=/etc/kubernetes/scheduler-config.yaml
```

> **Managed clusters (EKS, GKE, AKS):** The kube-scheduler is typically not accessible for custom extender configuration. As a workaround, deploy a mutating admission webhook that injects a `nodeSelector` on pods whose PVCs carry `agent.cubbit.io/disk-uuid`, using the node labels already set by the scanner.

### 3. Verify the scanner is running

```bash
kubectl -n kube-system get ds local-disk-node-scanner
kubectl -n kube-system logs -l app=local-disk-node-scanner

# Check that nodes are labeled
kubectl get nodes --show-labels | grep has-uuid
```

### 4. Use a UUID-based PVC

Find the UUID of the disk you want:

```bash
# On the worker node
lsblk -o NAME,UUID,SIZE,FSTYPE
# or
blkid
```

Create the PVC:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: agent-data
  annotations:
    agent.cubbit.io/disk-uuid: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
spec:
  storageClassName: local-disk
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 100Gi
```

See `deploy/example/` for a complete PVC + StatefulSet example.

## Configuration

### node-scanner flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scan-interval` | `60s` | How often to re-scan for block devices |
| `--mount-base` | `/mnt/cubbit` | Base directory for mounts (`<base>/<uuid>`) |
| `--fs-type` | `ext4` | Filesystem type to look for / format with |
| `--min-size` | `52428800` | Minimum device size in bytes (50 MiB) |

`NODE_NAME` must be set via the downward API (already done in `deploy/daemonset.yaml`).

### provisioner flags

Standard `klog` flags are accepted (e.g. `-v=4` for verbose logging).

### scheduler-extender flags

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8888` | HTTP listen address |

## Labels and Annotations

| Key | Set by | Used by | Description |
|-----|--------|---------|-------------|
| `agent.cubbit.io/has-uuid-<UUID>` | node-scanner | scheduler-extender | Node has disk with this UUID mounted |
| `agent.cubbit.io/disk-uuid-map` | node-scanner | provisioner | JSON map of UUID → path + size |
| `agent.cubbit.io/disk-uuid` | user (on PVC) | provisioner, extender | The UUID to claim |
| `agent.cubbit.io/provisioned-by` | provisioner (on PV) | provisioner Delete() | Ownership marker |

**Label key length:** `agent.cubbit.io/has-uuid-` (25 chars prefix) + 36-char UUID = 61 chars in the name segment, safely under the 63-char Kubernetes limit.

## Building

```bash
# Build all binaries
make build

# Run tests
make test

# Build Docker images
make docker-build REGISTRY=your-registry

# Push images
make docker-push REGISTRY=your-registry VERSION=v1.0.0
```

## Edge Cases

| Scenario | Behaviour |
|----------|-----------|
| Disk disappears between scans | Scanner removes the node label and annotation entry. The PV remains but the pod will get I/O errors (hardware failure — unavoidable). |
| Node goes down | Pod stays `Pending` due to `nodeAffinity`. Resumes when the node comes back. Inherent to local volumes. |
| UUID not found on any node | Pod stays `Pending`. Describe the pod to see the extender rejection reason. |
| Two PVCs requesting the same UUID | Both will attempt provisioning; the second will fail because the disk is already bound. |
| PVC created before scanner has run | PVC stays `Pending` until the scanner labels the node. No action needed. |
| Raw (unformatted) disk | `driver-init` formats it automatically on the next scanner cycle using `mkfs.ext4`. |

## Development

```bash
# Install dependencies
go mod download

# Run unit tests
go test ./internal/... -v -race

# Lint (requires golangci-lint)
make lint
```

The `internal/scanner` package requires a real Linux system with `lsblk` and `mount` for integration testing. Unit tests for the provisioner and scheduler extender use the `fake.NewSimpleClientset()` from `k8s.io/client-go` and do not require a running cluster.
