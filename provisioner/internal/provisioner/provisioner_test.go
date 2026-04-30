package provisioner_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/commonlib"
	"github.com/cubbitgg/kubernetes-external-provider/provisioner/internal/provisioner"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v13/controller"
)

func TestProvision_Success(t *testing.T) {
	diskMap := map[string]commonlib.DiskEntry{
		"test-uuid-1234": {Path: "/mnt/cubbit/test-uuid-1234", Size: 1073741824},
	}
	diskMapJSON, _ := json.Marshal(diskMap)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
			Annotations: map[string]string{
				commonlib.AnnotationDiskMap: string(diskMapJSON),
			},
		},
	}

	retain := corev1.PersistentVolumeReclaimRetain
	sc := &storagev1.StorageClass{
		ObjectMeta:    metav1.ObjectMeta{Name: "local-disk"},
		ReclaimPolicy: &retain,
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pvc",
			Namespace: "default",
			Annotations: map[string]string{
				commonlib.PVCAnnotationUUID: "test-uuid-1234",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}

	client := fake.NewSimpleClientset(node)
	p := provisioner.New(client)

	pv, state, err := p.Provision(context.Background(), controller.ProvisionOptions{
		PVName:           "pv-test",
		StorageClass:     sc,
		PVC:              pvc,
		SelectedNodeName: "worker-1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != controller.ProvisioningFinished {
		t.Errorf("expected ProvisioningFinished, got %v", state)
	}
	if pv.Spec.Local.Path != "/mnt/cubbit/test-uuid-1234" {
		t.Errorf("unexpected path: %s", pv.Spec.Local.Path)
	}
	if pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values[0] != "worker-1" {
		t.Error("nodeAffinity not set to worker-1")
	}
	if pv.Annotations[commonlib.PVCAnnotationUUID] != "test-uuid-1234" {
		t.Error("UUID annotation not set on PV")
	}
}

func TestProvision_MissingUUIDAnnotation(t *testing.T) {
	client := fake.NewSimpleClientset()
	p := provisioner.New(client)

	retain := corev1.PersistentVolumeReclaimRetain
	_, state, err := p.Provision(context.Background(), controller.ProvisionOptions{
		PVName: "pv-test",
		StorageClass: &storagev1.StorageClass{
			ObjectMeta:    metav1.ObjectMeta{Name: "local-disk"},
			ReclaimPolicy: &retain,
		},
		PVC: &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc", Namespace: "default"},
		},
		SelectedNodeName: "worker-1",
	})

	if err == nil {
		t.Fatal("expected error for missing UUID annotation")
	}
	if state != controller.ProvisioningFinished {
		t.Errorf("expected ProvisioningFinished, got %v", state)
	}
}

func TestProvision_UUIDNotOnNode_Reschedule(t *testing.T) {
	diskMap := map[string]commonlib.DiskEntry{
		"other-uuid": {Path: "/mnt/cubbit/other-uuid", Size: 500000000},
	}
	diskMapJSON, _ := json.Marshal(diskMap)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
			Annotations: map[string]string{
				commonlib.AnnotationDiskMap: string(diskMapJSON),
			},
		},
	}

	retain := corev1.PersistentVolumeReclaimRetain
	client := fake.NewSimpleClientset(node)
	p := provisioner.New(client)

	_, state, err := p.Provision(context.Background(), controller.ProvisionOptions{
		PVName: "pv-test",
		StorageClass: &storagev1.StorageClass{
			ObjectMeta:    metav1.ObjectMeta{Name: "local-disk"},
			ReclaimPolicy: &retain,
		},
		PVC: &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc",
				Namespace: "default",
				Annotations: map[string]string{
					commonlib.PVCAnnotationUUID: "wanted-uuid",
				},
			},
		},
		SelectedNodeName: "worker-1",
	})

	if err == nil {
		t.Fatal("expected error for missing UUID on node")
	}
	if state != controller.ProvisioningReschedule {
		t.Errorf("expected ProvisioningReschedule, got %v", state)
	}
}

func TestProvision_NoSelectedNode(t *testing.T) {
	client := fake.NewSimpleClientset()
	p := provisioner.New(client)

	retain := corev1.PersistentVolumeReclaimRetain
	_, state, err := p.Provision(context.Background(), controller.ProvisionOptions{
		PVName: "pv-test",
		StorageClass: &storagev1.StorageClass{
			ObjectMeta:    metav1.ObjectMeta{Name: "local-disk"},
			ReclaimPolicy: &retain,
		},
		PVC: &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc", Namespace: "default"},
		},
		SelectedNodeName: "", // not set
	})

	if err == nil {
		t.Fatal("expected error for missing selected node")
	}
	if state != controller.ProvisioningFinished {
		t.Errorf("expected ProvisioningFinished, got %v", state)
	}
}

func TestDelete_OwnedPV(t *testing.T) {
	client := fake.NewSimpleClientset()
	p := provisioner.New(client)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-1",
			Annotations: map[string]string{
				commonlib.AnnotationProvisionedBy: commonlib.ProvisionerName,
			},
		},
	}

	if err := p.Delete(context.Background(), pv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelete_ForeignPV(t *testing.T) {
	client := fake.NewSimpleClientset()
	p := provisioner.New(client)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-foreign",
			Annotations: map[string]string{
				commonlib.AnnotationProvisionedBy: "other.provisioner.io",
			},
		},
	}

	err := p.Delete(context.Background(), pv)
	if err == nil {
		t.Fatal("expected IgnoredError for foreign PV")
	}
	if _, ok := err.(*controller.IgnoredError); !ok {
		t.Errorf("expected *controller.IgnoredError, got %T: %v", err, err)
	}
}
