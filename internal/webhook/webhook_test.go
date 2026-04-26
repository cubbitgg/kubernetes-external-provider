package webhook_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/internal/common"
	"github.com/cubbitgg/kubernetes-external-provider/internal/webhook"
	"github.com/rs/zerolog"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const testUUID = "aaaabbbb-cccc-dddd-eeee-ffffffffffff"

func makePVCLister(pvcs ...*corev1.PersistentVolumeClaim) corelisters.PersistentVolumeClaimLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, pvc := range pvcs {
		_ = indexer.Add(pvc)
	}
	return corelisters.NewPersistentVolumeClaimLister(indexer)
}

func makeNodeLister(nodes ...*corev1.Node) corelisters.NodeLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, n := range nodes {
		_ = indexer.Add(n)
	}
	return corelisters.NewNodeLister(indexer)
}

func callHandle(t *testing.T, wh *webhook.Webhook, pod *corev1.Pod) *admissionv1.AdmissionReview {
	t.Helper()
	raw, _ := json.Marshal(pod)
	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Request: &admissionv1.AdmissionRequest{
			UID:    types.UID("test-uid"),
			Object: runtime.RawExtension{Raw: raw},
		},
	}
	body, _ := json.Marshal(review)
	req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	wh.Handle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp admissionv1.AdmissionReview
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return &resp
}

func TestHandle_MatchingNode(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-pvc",
			Namespace:   "default",
			Annotations: map[string]string{common.PVCAnnotationUUID: testUUID},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-with-disk",
			Labels: map[string]string{common.LabelUUIDPrefix + testUUID: "true"},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
				},
			}},
		},
	}

	wh := webhook.New(fake.NewSimpleClientset(pvc), makePVCLister(pvc), makeNodeLister(node), zerolog.Nop())
	resp := callHandle(t, wh, pod)

	if !resp.Response.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Response.Result)
	}
	if resp.Response.Patch == nil {
		t.Fatal("expected a JSON patch, got nil")
	}
	var ops []map[string]any
	if err := json.Unmarshal(resp.Response.Patch, &ops); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("expected at least one patch operation")
	}
	if ops[0]["path"] != "/spec/nodeSelector" {
		t.Errorf("expected patch path /spec/nodeSelector, got %s", ops[0]["path"])
	}
}

func TestHandle_NoUUIDAnnotation_PassThrough(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-pvc", Namespace: "default"},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "plain-pvc"},
				},
			}},
		},
	}

	wh := webhook.New(fake.NewSimpleClientset(pvc), makePVCLister(pvc), makeNodeLister(), zerolog.Nop())
	resp := callHandle(t, wh, pod)

	if !resp.Response.Allowed {
		t.Fatalf("expected pass-through Allowed=true, got false")
	}
	if resp.Response.Patch != nil {
		t.Errorf("expected no patch for plain PVC, got: %s", resp.Response.Patch)
	}
}

func TestHandle_NoPVCVolumes_PassThrough(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec:       corev1.PodSpec{Volumes: []corev1.Volume{{Name: "configmap-vol"}}},
	}

	wh := webhook.New(fake.NewSimpleClientset(), makePVCLister(), makeNodeLister(), zerolog.Nop())
	resp := callHandle(t, wh, pod)

	if !resp.Response.Allowed {
		t.Fatalf("expected pass-through Allowed=true, got false")
	}
	if resp.Response.Patch != nil {
		t.Errorf("expected no patch for pod with no PVC volumes, got: %s", resp.Response.Patch)
	}
}

func TestHandle_NoNodeFound_Rejected(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-pvc",
			Namespace:   "default",
			Annotations: map[string]string{common.PVCAnnotationUUID: testUUID},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
				},
			}},
		},
	}

	wh := webhook.New(fake.NewSimpleClientset(pvc), makePVCLister(pvc), makeNodeLister(), zerolog.Nop())
	resp := callHandle(t, wh, pod)

	if resp.Response.Allowed {
		t.Fatal("expected Allowed=false when no node has the UUID label")
	}
}

func TestHandle_ExistingNodeSelector_Merged(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-pvc",
			Namespace:   "default",
			Annotations: map[string]string{common.PVCAnnotationUUID: testUUID},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-with-disk",
			Labels: map[string]string{common.LabelUUIDPrefix + testUUID: "true"},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
				},
			}},
			NodeSelector: map[string]string{"disktype": "ssd"},
		},
	}

	wh := webhook.New(fake.NewSimpleClientset(pvc), makePVCLister(pvc), makeNodeLister(node), zerolog.Nop())
	resp := callHandle(t, wh, pod)

	if !resp.Response.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Response.Result)
	}
	if resp.Response.Patch == nil {
		t.Fatal("expected a JSON patch, got nil")
	}
	var ops []map[string]any
	if err := json.Unmarshal(resp.Response.Patch, &ops); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 patch op, got %d: %v", len(ops), ops)
	}
	// '/' in the key is escaped as '~1' per RFC 6901.
	expected := "/spec/nodeSelector/kubernetes.io~1hostname"
	if ops[0]["path"] != expected {
		t.Errorf("expected patch path %s, got %s", expected, ops[0]["path"])
	}
	if ops[0]["value"] != "node-with-disk" {
		t.Errorf("expected patch value node-with-disk, got %v", ops[0]["value"])
	}
}

func TestHandle_ExistingAffinity_NodeSelectorAdded(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-pvc",
			Namespace:   "default",
			Annotations: map[string]string{common.PVCAnnotationUUID: testUUID},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-with-disk",
			Labels: map[string]string{common.LabelUUIDPrefix + testUUID: "true"},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
				},
			}},
			// Pod has existing affinity but no nodeSelector: webhook adds nodeSelector independently.
			Affinity: &corev1.Affinity{
				PodAffinity: &corev1.PodAffinity{},
			},
		},
	}

	wh := webhook.New(fake.NewSimpleClientset(pvc), makePVCLister(pvc), makeNodeLister(node), zerolog.Nop())
	resp := callHandle(t, wh, pod)

	if !resp.Response.Allowed {
		t.Fatalf("expected Allowed=true, got false: %v", resp.Response.Result)
	}
	var ops []map[string]any
	if err := json.Unmarshal(resp.Response.Patch, &ops); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if len(ops) != 1 || ops[0]["path"] != "/spec/nodeSelector" {
		t.Errorf("expected single patch at /spec/nodeSelector, got: %v", ops)
	}
}
