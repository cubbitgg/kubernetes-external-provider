# Architecture

## Components

| Component | Kind | Purpose |
|-----------|------|---------|
| `node-scanner` | DaemonSet | Discovers disks, mounts them, labels and annotates nodes |
| `provisioner` | Deployment (leader election) | Creates `PersistentVolumes` on demand |
| `webhook` | Deployment (2 replicas) | Mutating admission webhook — injects `nodeAffinity` into pods |

```
Worker Node                         Control Plane
┌──────────────────────────┐        ┌─────────────────────────────────────┐
│  node-scanner (DaemonSet)│        │                                     │
│                          │        │  ┌─────────────────────────────┐    │
│  formats raw disks       │─labels/│  │  provisioner                │    │
│  mounts /mnt/cubbit/<id> │─annots▶│  │  creates PVs on PVC bind    │    │
│                          │        │  └─────────────────────────────┘    │
└──────────────────────────┘        │                                     │
                                    │  ┌─────────────────────────────┐    │
                                    │  │  webhook (mutating)          │    │
                                    │  │  injects nodeAffinity        │    │
                                    │  │  on pod CREATE               │    │
                                    │  └─────────────────────────────┘    │
                                    │                                     │
                                    │  kube-scheduler (unmodified)        │
                                    └─────────────────────────────────────┘
```

## End-to-end flow

1. **node-scanner** runs on every node (60 s loop by default):
   - Formats raw block devices with ext4 (if unformatted).
   - Mounts each formatted disk at `/mnt/cubbit/<uuid>`.
   - Patches the node with:
     - Label `agent.cubbit.io/has-uuid-<UUID>: "true"` for each mounted disk.
     - Annotation `agent.cubbit.io/disk-uuid-map: {"<uuid>": {"path": "...", "size": <bytes>}}`.

2. **User creates a PVC** with annotation `agent.cubbit.io/disk-uuid: <UUID>` and `storageClassName: local-disk`.

3. **User creates a Pod/StatefulSet** referencing that PVC.

4. **Mutating webhook** intercepts the pod `CREATE`:
   - Looks up the PVC to read the UUID annotation.
   - Finds the node with label `agent.cubbit.io/has-uuid-<UUID>=true`.
   - Injects `nodeAffinity` (requiredDuringSchedulingIgnoredDuringExecution) into the pod spec, pinning it to that node.
   - If no node has the UUID: **rejects** the pod with a clear error message. The controller (StatefulSet/ReplicaSet) will retry.

5. **kube-scheduler** places the pod on the constrained node (no custom configuration required).

6. Because `volumeBindingMode: WaitForFirstConsumer`, the PVC is now bound to the selected node (`SelectedNodeName` is set).

7. **provisioner** sees the bound PVC:
   - Reads the UUID from the PVC annotation.
   - Reads the disk path and size from the node's `disk-uuid-map` annotation.
   - Creates the `PersistentVolume` with `spec.local.path` and `spec.nodeAffinity` pinned to the node.

8. Pod starts with the disk mounted.

## Node labels and annotations

| Key | Type | Set by | Used by |
|-----|------|--------|---------|
| `agent.cubbit.io/has-uuid-<UUID>` | Node label | node-scanner | webhook |
| `agent.cubbit.io/disk-uuid-map` | Node annotation (JSON) | node-scanner | provisioner |
| `agent.cubbit.io/disk-uuid` | PVC annotation | user | webhook, provisioner |
| `agent.cubbit.io/provisioned-by` | PV annotation | provisioner | provisioner Delete() |

The disk-map annotation is a JSON object: `{"<uuid>": {"path": "/mnt/cubbit/<uuid>", "size": 1073741824}}`.

Label key length: `agent.cubbit.io/has-uuid-` (25 chars) + 36-char UUID = 61 chars — within the 63-char Kubernetes limit.

## Webhook TLS bootstrap

The webhook binary uses `--self-sign` by default. On startup it:

1. Checks for an existing Secret `local-disk-webhook-tls` in `kube-system`.
2. If absent, generates a self-signed CA and server certificate (10-year validity, SANs for the service DNS names).
3. Stores them in the Secret.
4. Patches the `MutatingWebhookConfiguration` with the CA bundle and switches `failurePolicy` from `Ignore` to `Fail`.

The `MutatingWebhookConfiguration` is deployed with `failurePolicy: Ignore` and an empty `caBundle` so that applying the manifest does not immediately block pod creation. The webhook binary sets both correctly within seconds of starting.

To use cert-manager instead, omit `--self-sign`, set `--tls-cert` and `--tls-key` to cert-manager-managed paths, and populate `caBundle` via a cert-manager `Certificate` with `spec.issuerRef`.

## Design decisions

**Why WaitForFirstConsumer?**
`WaitForFirstConsumer` delays PV creation until a pod is scheduled. This gives the webhook time to inject `nodeAffinity` before the provisioner runs, ensuring `SelectedNodeName` is always set.

**Why Retain reclaim policy?**
Deleting a PVC should never erase physical disk data. Disks can be re-claimed by creating a new PVC with the same UUID annotation.

**Why not ProvisioningInBackground?**
Provisioning completes synchronously: the provisioner knows the node, path, and size from the annotations. No background work is needed.

**Why nodeAffinity AND semantics?**
`nodeSelectorTerms` are OR-ed; appending a new term would loosen existing pod constraints. The webhook instead appends a `matchExpression` to each existing term, preserving AND semantics with any user-set affinity rules.

## Edge cases

| Scenario | Behaviour |
|----------|-----------|
| Disk disappears between scans | Scanner removes the label. The next pod creation for that UUID is rejected. Running pods get I/O errors (hardware failure). |
| Node goes down | Pod stays `Pending` due to `nodeAffinity`. Resumes when the node returns. Inherent to local volumes. |
| UUID not found on any node | Webhook rejects the pod. Describe the pod events for the rejection reason. |
| Two PVCs requesting the same UUID | The second provisioning attempt fails — the disk is already bound to a PV. |
| PVC created before scanner has run | Webhook rejects the pod; StatefulSet/ReplicaSet retries until the scanner labels the node. |
| Raw (unformatted) disk | `driver-init` formats it automatically on the next scanner cycle. |
| Multiple replicas racing to create the TLS secret | Only one Create wins; others fall back to Get. All replicas end up using the same cert. |
