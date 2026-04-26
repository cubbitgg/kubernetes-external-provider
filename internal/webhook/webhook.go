package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cubbitgg/kubernetes-external-provider/internal/common"
	"github.com/rs/zerolog"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
)

// Webhook is an HTTP handler that implements the mutating admission webhook for pod scheduling.
// It injects nodeAffinity into pods whose PVCs request a specific disk UUID,
// pinning them to the node that physically holds that disk.
type Webhook struct {
	client     kubernetes.Interface
	pvcLister  corelisters.PersistentVolumeClaimLister
	nodeLister corelisters.NodeLister
	log        zerolog.Logger
}

// New returns a new Webhook handler.
func New(client kubernetes.Interface, pvcLister corelisters.PersistentVolumeClaimLister, nodeLister corelisters.NodeLister, log zerolog.Logger) *Webhook {
	return &Webhook{
		client:     client,
		pvcLister:  pvcLister,
		nodeLister: nodeLister,
		log:        log,
	}
}

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// Handle processes POST /mutate requests from the Kubernetes API server.
func (wh *Webhook) Handle(w http.ResponseWriter, r *http.Request) {
	var review admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		http.Error(w, fmt.Sprintf("decode AdmissionReview: %v", err), http.StatusBadRequest)
		return
	}
	if review.Request == nil {
		http.Error(w, "missing request in AdmissionReview", http.StatusBadRequest)
		return
	}

	review.Response = wh.mutate(r.Context(), review.Request)
	review.Request = nil

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(review); err != nil {
		wh.log.Error().Err(err).Msg("Failed to write admission response")
	}
}

func (wh *Webhook) mutate(ctx context.Context, req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		return deny(req.UID, fmt.Sprintf("failed to decode pod: %v", err))
	}

	uuid, err := wh.diskUUIDForPod(ctx, &pod)
	if err != nil {
		wh.log.Error().Err(err).Str("pod", pod.Name).Msg("Failed to extract disk UUID from pod PVCs")
		return deny(req.UID, fmt.Sprintf("failed to resolve disk UUID: %v", err))
	}

	if uuid == "" {
		return allow(req.UID)
	}

	nodeName, err := wh.findNodeForUUID(uuid)
	if err != nil {
		wh.log.Error().Err(err).Str("uuid", uuid).Msg("Failed to list nodes for disk UUID")
		return deny(req.UID, fmt.Sprintf("failed to find node for disk UUID %s: %v", uuid, err))
	}
	if nodeName == "" {
		wh.log.Warn().Str("uuid", uuid).Str("pod", pod.Name).Msg("No node found with requested disk UUID")
		return deny(req.UID, fmt.Sprintf(
			"no node has disk with UUID %s (label %s%s not found on any node)",
			uuid, common.LabelUUIDPrefix, uuid,
		))
	}

	patch, err := buildNodeSelectorPatch(&pod, nodeName)
	if err != nil {
		wh.log.Error().Err(err).Str("pod", pod.Name).Msg("Failed to build nodeSelector patch")
		return deny(req.UID, fmt.Sprintf("failed to build nodeSelector patch: %v", err))
	}

	wh.log.Info().
		Str("pod", pod.Name).
		Str("namespace", pod.Namespace).
		Str("uuid", uuid).
		Str("node", nodeName).
		Msg("Injecting nodeSelector into pod")

	pt := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		UID:       req.UID,
		Allowed:   true,
		Patch:     patch,
		PatchType: &pt,
	}
}

// diskUUIDForPod returns the disk UUID from the first UUID-annotated PVC referenced by the pod,
// or "" if no such PVC exists.
// If multiple PVCs specify different UUIDs, the first one found wins (V1 limitation).
func (wh *Webhook) diskUUIDForPod(ctx context.Context, pod *corev1.Pod) (string, error) {
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim == nil {
			continue
		}
		pvcName := vol.PersistentVolumeClaim.ClaimName
		namespace := pod.Namespace

		pvc, err := wh.pvcLister.PersistentVolumeClaims(namespace).Get(pvcName)
		if err != nil {
			// Fall back to a live API call if the informer cache misses.
			livePVC, apiErr := wh.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
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

// findNodeForUUID returns the name of the node carrying the UUID label, or "" if none found.
func (wh *Webhook) findNodeForUUID(uuid string) (string, error) {
	labelKey := common.LabelUUIDPrefix + uuid
	sel := labels.SelectorFromSet(labels.Set{labelKey: "true"})
	nodes, err := wh.nodeLister.List(sel)
	if err != nil {
		return "", fmt.Errorf("list nodes with label %s: %w", labelKey, err)
	}
	if len(nodes) == 0 {
		return "", nil
	}
	return nodes[0].Name, nil
}

// buildNodeSelectorPatch generates a RFC 6902 JSON patch that sets kubernetes.io/hostname
// in the pod's nodeSelector, pinning it to the given node.
func buildNodeSelectorPatch(pod *corev1.Pod, nodeName string) ([]byte, error) {
	var ops []patchOp

	if pod.Spec.NodeSelector == nil {
		ops = append(ops, patchOp{
			Op:    "add",
			Path:  "/spec/nodeSelector",
			Value: map[string]string{"kubernetes.io/hostname": nodeName},
		})
	} else {
		// '/' in a JSON Pointer key segment is escaped as '~1' (RFC 6901).
		ops = append(ops, patchOp{
			Op:    "add",
			Path:  "/spec/nodeSelector/kubernetes.io~1hostname",
			Value: nodeName,
		})
	}

	return json.Marshal(ops)
}

func allow(uid types.UID) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{UID: uid, Allowed: true}
}

func deny(uid types.UID, message string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		UID:     uid,
		Allowed: false,
		Result:  &metav1.Status{Code: http.StatusForbidden, Message: message},
	}
}
