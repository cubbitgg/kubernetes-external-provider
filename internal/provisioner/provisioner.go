package provisioner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cubbitgg/kubernetes-external-provider/internal/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v13/controller"
)

// DiskEntry is the value type stored in the node annotation disk map.
type DiskEntry struct {
	Path string `json:"path"`
	Size uint64 `json:"size"`
}

// LocalDiskProvisioner implements controller.Provisioner.
// It creates local PVs pinned to the node that physically holds the requested disk UUID.
type LocalDiskProvisioner struct {
	client kubernetes.Interface
}

// New returns a new LocalDiskProvisioner.
func New(client kubernetes.Interface) *LocalDiskProvisioner {
	return &LocalDiskProvisioner{client: client}
}

// Provision is called by the controller when a new PVC needs a PV.
// It expects:
//   - StorageClass.volumeBindingMode = WaitForFirstConsumer (so SelectedNode is set)
//   - PVC annotation agent.cubbit.io/disk-uuid = <target UUID>
//   - The selected node to carry annotation agent.cubbit.io/disk-uuid-map
func (p *LocalDiskProvisioner) Provision(ctx context.Context, options controller.ProvisionOptions) (*corev1.PersistentVolume, controller.ProvisioningState, error) {
	nodeName := options.SelectedNodeName
	if nodeName == "" {
		return nil, controller.ProvisioningFinished, fmt.Errorf(
			"no selected node for PVC %s/%s; StorageClass must use WaitForFirstConsumer binding mode",
			options.PVC.Namespace, options.PVC.Name,
		)
	}

	uuid := options.PVC.Annotations[common.PVCAnnotationUUID]
	if uuid == "" {
		return nil, controller.ProvisioningFinished, fmt.Errorf(
			"PVC %s/%s missing required annotation %s",
			options.PVC.Namespace, options.PVC.Name, common.PVCAnnotationUUID,
		)
	}

	node, err := p.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, controller.ProvisioningFinished, fmt.Errorf("get node %s: %w", nodeName, err)
	}

	diskMapJSON := node.Annotations[common.AnnotationDiskMap]
	if diskMapJSON == "" {
		// Node not yet annotated by the scanner; reschedule so the scheduler
		// can pick a node that has been scanned.
		return nil, controller.ProvisioningReschedule, fmt.Errorf(
			"node %s has no %s annotation; scanner may not have run yet",
			nodeName, common.AnnotationDiskMap,
		)
	}

	var diskMap map[string]DiskEntry
	if err := json.Unmarshal([]byte(diskMapJSON), &diskMap); err != nil {
		return nil, controller.ProvisioningFinished, fmt.Errorf(
			"parse disk map annotation on node %s: %w", nodeName, err,
		)
	}

	diskInfo, ok := diskMap[uuid]
	if !ok {
		// The scheduler extender should have prevented this, but handle it
		// defensively: ask the scheduler to try a different node.
		return nil, controller.ProvisioningReschedule, fmt.Errorf(
			"UUID %s not found in disk map on node %s", uuid, nodeName,
		)
	}

	// Use the actual disk size if the scanner provided it; fall back to PVC request.
	capacity := options.PVC.Spec.Resources.Requests[corev1.ResourceStorage]
	if diskInfo.Size > 0 {
		capacity = *resource.NewQuantity(int64(diskInfo.Size), resource.BinarySI)
	}

	reclaimPolicy := corev1.PersistentVolumeReclaimRetain
	if options.StorageClass.ReclaimPolicy != nil {
		reclaimPolicy = *options.StorageClass.ReclaimPolicy
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
			Annotations: map[string]string{
				common.AnnotationProvisionedBy: common.ProvisionerName,
				common.PVCAnnotationUUID:       uuid,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: reclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			StorageClassName:              options.StorageClass.Name,
			VolumeMode:                    options.PVC.Spec.VolumeMode,
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: capacity,
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				Local: &corev1.LocalVolumeSource{
					Path: diskInfo.Path,
				},
			},
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      "kubernetes.io/hostname",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{nodeName},
						}},
					}},
				},
			},
		},
	}

	return pv, controller.ProvisioningFinished, nil
}

// Delete is called when a Released PV's reclaim policy is Delete.
// We only remove our ownership annotation; the underlying disk is left intact.
func (p *LocalDiskProvisioner) Delete(_ context.Context, pv *corev1.PersistentVolume) error {
	if pv.Annotations[common.AnnotationProvisionedBy] != common.ProvisionerName {
		return &controller.IgnoredError{
			Reason: fmt.Sprintf("PV %s was not provisioned by %s", pv.Name, common.ProvisionerName),
		}
	}
	// No physical cleanup: the disk persists and can be reused by a new PVC.
	return nil
}
