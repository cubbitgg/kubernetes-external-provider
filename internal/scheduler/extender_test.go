package scheduler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/internal/common"
	"github.com/cubbitgg/kubernetes-external-provider/internal/scheduler"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	corelisters "k8s.io/client-go/listers/core/v1"
)

const testUUID = "aaaabbbb-cccc-dddd-eeee-ffffffffffff"

func makePVCLister(pvcs ...*corev1.PersistentVolumeClaim) corelisters.PersistentVolumeClaimLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, pvc := range pvcs {
		_ = indexer.Add(pvc)
	}
	return corelisters.NewPersistentVolumeClaimLister(indexer)
}

func buildFilterArgs(pod *corev1.Pod, nodes []corev1.Node) scheduler.ExtenderArgs {
	nodeList := &corev1.NodeList{Items: nodes}
	return scheduler.ExtenderArgs{Pod: pod, Nodes: nodeList}
}

func callFilter(t *testing.T, ext *scheduler.Extender, args scheduler.ExtenderArgs) scheduler.ExtenderFilterResult {
	t.Helper()
	body, _ := json.Marshal(args)
	req := httptest.NewRequest(http.MethodPost, "/filter", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	ext.Filter(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result scheduler.ExtenderFilterResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func TestFilter_MatchingNode(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pvc",
			Namespace: "default",
			Annotations: map[string]string{
				common.PVCAnnotationUUID: testUUID,
			},
		},
	}

	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-with-disk",
				Labels: map[string]string{common.LabelUUIDPrefix + testUUID: "true"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-without-disk",
				Labels: map[string]string{},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
				},
			}},
		},
	}

	ext := scheduler.NewExtender(fake.NewSimpleClientset(pvc), makePVCLister(pvc))
	result := callFilter(t, ext, buildFilterArgs(pod, nodes))

	if len(result.Nodes.Items) != 1 || result.Nodes.Items[0].Name != "node-with-disk" {
		t.Errorf("expected node-with-disk to pass, got: %+v", result.Nodes)
	}
	if _, ok := result.FailedNodes["node-without-disk"]; !ok {
		t.Error("expected node-without-disk in failed nodes")
	}
}

func TestFilter_NoUUIDAnnotation_PassThrough(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-pvc", Namespace: "default"},
	}

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "plain-pvc"},
				},
			}},
		},
	}

	ext := scheduler.NewExtender(fake.NewSimpleClientset(pvc), makePVCLister(pvc))
	result := callFilter(t, ext, buildFilterArgs(pod, nodes))

	if len(result.Nodes.Items) != 2 {
		t.Errorf("expected all 2 nodes to pass through, got %d", len(result.Nodes.Items))
	}
}

func TestFilter_NoPVCVolumes_PassThrough(t *testing.T) {
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec:       corev1.PodSpec{Volumes: []corev1.Volume{{Name: "configmap-vol"}}},
	}

	ext := scheduler.NewExtender(fake.NewSimpleClientset(), makePVCLister())
	result := callFilter(t, ext, buildFilterArgs(pod, nodes))

	if len(result.Nodes.Items) != 1 {
		t.Errorf("expected 1 node to pass through, got %d", len(result.Nodes.Items))
	}
}

func TestFilter_AllNodesRejected(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pvc",
			Namespace: "default",
			Annotations: map[string]string{
				common.PVCAnnotationUUID: testUUID,
			},
		},
	}

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
				},
			}},
		},
	}

	ext := scheduler.NewExtender(fake.NewSimpleClientset(pvc), makePVCLister(pvc))
	result := callFilter(t, ext, buildFilterArgs(pod, nodes))

	if len(result.Nodes.Items) != 0 {
		t.Errorf("expected 0 eligible nodes, got %d", len(result.Nodes.Items))
	}
	if len(result.FailedNodes) != 2 {
		t.Errorf("expected 2 failed nodes, got %d", len(result.FailedNodes))
	}
}
