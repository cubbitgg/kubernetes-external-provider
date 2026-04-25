# kubernetes-external-provider

Provisions local Kubernetes `PersistentVolumes` bound to a specific physical disk by UUID — without hardcoding node names. The system auto-discovers which node holds the disk and schedules pods there automatically.

> Simpler than a full CSI driver, inspired by [local-path-provisioner](https://github.com/rancher/local-path-provisioner).

## How it works

1. A **DaemonSet** on each node scans for disks, mounts them under `/mnt/cubbit/<uuid>`, and labels the node.
2. When a pod needs a disk, a **mutating webhook** reads the UUID from the PVC annotation and injects the correct `nodeAffinity` into the pod spec.
3. The **provisioner** creates the `PersistentVolume` pointing to the mounted disk path on that node.

No kube-scheduler configuration required — works on managed clusters (EKS, GKE, AKS).

## Deploy

**Prerequisites:** Kubernetes ≥ 1.29, nodes with block devices, `lsblk` available on hosts.

```bash
kubectl apply -f deploy/rbac.yaml
kubectl apply -f deploy/storageclass.yaml
kubectl apply -f deploy/daemonset.yaml
kubectl apply -f deploy/provisioner.yaml
kubectl apply -f deploy/webhook.yaml
```

Verify the scanner is running and nodes are labeled:

```bash
kubectl -n kube-system get ds local-disk-node-scanner
kubectl get nodes --show-labels | grep has-uuid
```

## Claim a disk

**1. Find the disk UUID on the node:**

```bash
lsblk -o NAME,UUID,SIZE,FSTYPE
# or: blkid
```

**2. Create a PVC with the UUID annotation:**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-disk
  annotations:
    agent.cubbit.io/disk-uuid: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
spec:
  storageClassName: local-disk
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 100Gi
```

**3. Reference it in your pod or StatefulSet:**

```yaml
volumes:
  - name: data
    persistentVolumeClaim:
      claimName: my-disk
```

The pod will be scheduled automatically on the node that holds the disk. See `deploy/example/` for a complete StatefulSet example.

## Notes

- **Disk data is never deleted** — `reclaimPolicy: Retain` is intentional. Deleting a PVC does not wipe the disk.
- **Raw disks are formatted automatically** — the scanner formats unformatted block devices with ext4 on first discovery.
- If the webhook hasn't started yet when you create the first pod, the pod will be rejected and retried. It will succeed once the webhook is ready (typically within a few seconds of deployment).

## Further reading

- [Architecture & data flow](docs/architecture.md)
- [Configuration reference](docs/configuration.md)
