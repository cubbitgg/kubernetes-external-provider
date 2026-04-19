package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cubbitgg/kubernetes-external-provider/internal/common"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
)

// The types below mirror the Kubernetes scheduler extender protocol.
// Defined locally to avoid depending on the large k8s.io/kube-scheduler module.

// ExtenderArgs is the input sent by the scheduler to the extender's filter endpoint.
type ExtenderArgs struct {
	Pod       *corev1.Pod      `json:"pod"`
	Nodes     *corev1.NodeList `json:"nodes,omitempty"`
	NodeNames *[]string        `json:"nodenames,omitempty"`
}

// ExtenderFilterResult is the response the extender returns.
type ExtenderFilterResult struct {
	Nodes                      *corev1.NodeList `json:"nodes,omitempty"`
	NodeNames                  *[]string        `json:"nodenames,omitempty"`
	FailedNodes                FailedNodesMap   `json:"failedNodes,omitempty"`
	FailedAndUnresolvableNodes FailedNodesMap   `json:"failedAndUnresolvableNodes,omitempty"`
	Error                      string           `json:"error,omitempty"`
}

// FailedNodesMap maps node name -> reason for rejection.
type FailedNodesMap map[string]string

// Extender is an HTTP handler that implements the scheduler filter extension point.
// It filters out nodes that do not carry the label for the disk UUID requested by
// the pod's PVC(s).
type Extender struct {
	client    kubernetes.Interface
	pvcLister corelisters.PersistentVolumeClaimLister
	log       zerolog.Logger
}

// NewExtender returns a new Extender.
func NewExtender(client kubernetes.Interface, pvcLister corelisters.PersistentVolumeClaimLister, log zerolog.Logger) *Extender {
	return &Extender{client: client, pvcLister: pvcLister, log: log}
}

// Filter handles POST /filter requests from the Kubernetes scheduler.
func (e *Extender) Filter(w http.ResponseWriter, r *http.Request) {
	var args ExtenderArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return
	}
	if args.Pod == nil {
		http.Error(w, "missing pod in request", http.StatusBadRequest)
		return
	}

	uuid, err := e.diskUUIDForPod(r.Context(), args.Pod)
	if err != nil {
		e.log.Error().Err(err).Str("pod", args.Pod.Name).Msg("Failed to extract disk UUID from pod PVCs")
		// Don't block scheduling on extender errors; pass all nodes through.
		result := ExtenderFilterResult{Nodes: args.Nodes}
		writeJSON(e.log, w, result)
		return
	}

	if uuid == "" {
		// Pod has no UUID-based PVC; this extender has nothing to do.
		result := ExtenderFilterResult{Nodes: args.Nodes}
		writeJSON(e.log, w, result)
		return
	}

	labelKey := common.LabelUUIDPrefix + uuid
	filtered := &corev1.NodeList{}
	failed := make(FailedNodesMap)

	if args.Nodes != nil {
		for _, node := range args.Nodes.Items {
			if node.Labels[labelKey] == "true" {
				filtered.Items = append(filtered.Items, node)
			} else {
				failed[node.Name] = fmt.Sprintf("node does not have disk with UUID %s (label %s not set)", uuid, labelKey)
			}
		}
	}

	e.log.Debug().Str("uuid", uuid).Int("eligible", len(filtered.Items)).Int("rejected", len(failed)).Msg("Filter result")

	writeJSON(e.log, w, ExtenderFilterResult{
		Nodes:       filtered,
		FailedNodes: failed,
	})
}

// diskUUIDForPod returns the disk UUID required by the pod's PVCs, or "" if
// none of the pod's volumes use a UUID-annotated PVC.
// If multiple PVCs specify different UUIDs, the first one found wins (for V1).
func (e *Extender) diskUUIDForPod(ctx context.Context, pod *corev1.Pod) (string, error) {
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim == nil {
			continue
		}
		pvcName := vol.PersistentVolumeClaim.ClaimName
		namespace := pod.Namespace

		pvc, err := e.pvcLister.PersistentVolumeClaims(namespace).Get(pvcName)
		if err != nil {
			// Fall back to a live API call if the informer cache misses.
			livePVC, apiErr := e.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if apiErr != nil {
				return "", fmt.Errorf("get PVC %s/%s: %w", namespace, pvcName, apiErr)
			}
			pvc = livePVC
		}

		if uuid := pvc.Annotations[common.PVCAnnotationUUID]; uuid != "" {
			return uuid, nil
		}
	}
	return "", nil
}

func writeJSON(log zerolog.Logger, w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error().Err(err).Msg("Failed to write JSON response")
	}
}
