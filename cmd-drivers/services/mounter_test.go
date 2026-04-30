package services_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/models"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/services"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/tests/mocks"
)

// noopMounts returns a MockMountInfoProvider that reports no mounts (root not found → fail-open).
func noopMounts() *mocks.MockMountInfoProvider {
	return &mocks.MockMountInfoProvider{
		GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
			return nil, nil
		},
	}
}

func TestUnit_Mount_HappyPath(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb1", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return true, nil },
		MountFunc: func(source, target, fstype string, options []string) error {
			return nil
		},
	}
	lsblk := &mocks.MockLSBLK{}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir, FSType: "ext4"},
		resolver, mountProv, lsblk, noopMounts(),
	)
	if err := mounter.Mount(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestUnit_Mount_AlreadyMounted(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb1", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return false, nil }, // already mounted
	}
	lsblk := &mocks.MockLSBLK{}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir, FSType: "ext4"},
		resolver, mountProv, lsblk, noopMounts(),
	)
	if err := mounter.Mount(context.Background()); err != nil {
		t.Fatalf("expected no error (idempotent), got: %v", err)
	}
}

func TestUnit_Mount_ResolveError(t *testing.T) {
	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("not found")
		},
	}
	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: "550e8400-e29b-41d4-a716-446655440000", MountPoint: t.TempDir()},
		resolver, &mocks.MockK8sMountProvider{}, &mocks.MockLSBLK{}, noopMounts(),
	)
	if err := mounter.Mount(context.Background()); err == nil {
		t.Fatal("expected error from resolver, got nil")
	}
}

func TestUnit_Mount_MountError(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb1", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return true, nil },
		MountFunc: func(_, _, _ string, _ []string) error {
			return errors.New("mount failed")
		},
	}
	lsblk := &mocks.MockLSBLK{}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir, FSType: "ext4"},
		resolver, mountProv, lsblk, noopMounts(),
	)
	if err := mounter.Mount(context.Background()); err == nil {
		t.Fatal("expected mount error, got nil")
	}
}

func TestUnit_Mount_AutoDetectFSType(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb1", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return true, nil },
		MountFunc: func(_, _, fstype string, _ []string) error {
			if fstype != "ext4" {
				t.Errorf("expected fstype ext4, got %q", fstype)
			}
			return nil
		},
	}
	lsblk := &mocks.MockLSBLK{
		GetBlockDeviceFunc: func(_ context.Context, _ string) (*fsutils.BlockDevice, error) {
			return &fsutils.BlockDevice{FSType: "ext4"}, nil
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir}, // no FSType
		resolver, mountProv, lsblk, noopMounts(),
	)
	if err := mounter.Mount(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestUnit_Mount_NoFSTypeDetected(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb1", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return true, nil },
	}
	lsblk := &mocks.MockLSBLK{
		GetBlockDeviceFunc: func(_ context.Context, _ string) (*fsutils.BlockDevice, error) {
			return &fsutils.BlockDevice{FSType: ""}, nil // empty
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir},
		resolver, mountProv, lsblk, noopMounts(),
	)
	if err := mounter.Mount(context.Background()); err == nil {
		t.Fatal("expected error when fstype cannot be detected, got nil")
	}
}

func TestUnit_Mount_RefusesRootDiskPartition(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/nvme0n1p3", nil // same disk as root
		},
	}
	mountProv := &mocks.MockK8sMountProvider{}
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{
					Name: "/dev/nvme0n1", Type: "disk",
					Children: []fsutils.BlockDevice{
						{Name: "/dev/nvme0n1p1", Type: "part"},
						{Name: "/dev/nvme0n1p2", Type: "part"}, // root source
						{Name: "/dev/nvme0n1p3", Type: "part"}, // target — should be refused
					},
				},
			}, nil
		},
	}
	mounts := &mocks.MockMountInfoProvider{
		GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
			return []models.MountEntry{
				{Source: "/dev/nvme0n1p2", Mountpoint: "/", FSType: "ext4"},
			}, nil
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir, FSType: "ext4"},
		resolver, mountProv, lsblk, mounts,
	)
	err := mounter.Mount(context.Background())
	if err == nil {
		t.Fatal("expected error for root-disk partition, got nil")
	}
	if !contains(err.Error(), "root disk") {
		t.Errorf("expected 'root disk' in error, got: %v", err)
	}
}

func TestUnit_Mount_ManagedOnly_AcceptsMatchingLabel(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return true, nil },
		MountFunc:                 func(_, _, _ string, _ []string) error { return nil },
	}
	lsblk := &mocks.MockLSBLK{
		GetBlockDeviceFunc: func(_ context.Context, _ string) (*fsutils.BlockDevice, error) {
			return &fsutils.BlockDevice{FSType: "ext4", Label: "CUBBIT"}, nil
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir, ManagedOnly: true, RequireLabel: "CUBBIT"},
		resolver, mountProv, lsblk, noopMounts(),
	)
	if err := mounter.Mount(context.Background()); err != nil {
		t.Fatalf("expected no error for matching label, got: %v", err)
	}
}

func TestUnit_Mount_ManagedOnly_RejectsMismatchedLabel(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{}
	lsblk := &mocks.MockLSBLK{
		GetBlockDeviceFunc: func(_ context.Context, _ string) (*fsutils.BlockDevice, error) {
			return &fsutils.BlockDevice{FSType: "ext4", Label: "OTHER"}, nil
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir, ManagedOnly: true, RequireLabel: "CUBBIT"},
		resolver, mountProv, lsblk, noopMounts(),
	)
	err := mounter.Mount(context.Background())
	if err == nil {
		t.Fatal("expected error for mismatched label, got nil")
	}
	if !contains(err.Error(), "label") {
		t.Errorf("expected 'label' in error, got: %v", err)
	}
}

func TestUnit_Mount_NoRootDetected_ProceedsWithWarn(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	resolver := &mocks.MockDeviceResolver{
		ResolveUUIDFunc: func(_ context.Context, _ string) (string, error) {
			return "/dev/sdb", nil
		},
	}
	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return true, nil },
		MountFunc:                 func(_, _, _ string, _ []string) error { return nil },
	}
	lsblk := &mocks.MockLSBLK{}
	// mountinfo returns error → fail-open
	mountsErr := &mocks.MockMountInfoProvider{
		GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
			return nil, errors.New("proc not available")
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir, FSType: "ext4"},
		resolver, mountProv, lsblk, mountsErr,
	)
	if err := mounter.Mount(context.Background()); err != nil {
		t.Fatalf("expected fail-open when root detection fails, got: %v", err)
	}
}

func TestUnit_Unmount_HappyPath(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	target := filepath.Join(dir, uuid)
	if err := os.MkdirAll(target, 0750); err != nil {
		t.Fatal(err)
	}

	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return false, nil }, // is mounted
		UnmountFunc: func(_ string) error {
			return nil
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir},
		&mocks.MockDeviceResolver{}, mountProv, &mocks.MockLSBLK{}, noopMounts(),
	)
	if err := mounter.Unmount(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestUnit_Unmount_NotMounted(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return true, nil }, // not mounted
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir},
		&mocks.MockDeviceResolver{}, mountProv, &mocks.MockLSBLK{}, noopMounts(),
	)
	if err := mounter.Unmount(context.Background()); err != nil {
		t.Fatalf("expected no error (idempotent), got: %v", err)
	}
}

func TestUnit_Unmount_Error(t *testing.T) {
	dir := t.TempDir()
	uuid := "550e8400-e29b-41d4-a716-446655440000"

	if err := os.MkdirAll(filepath.Join(dir, uuid), 0750); err != nil {
		t.Fatalf("setup: mkdir: %v", err)
	}

	mountProv := &mocks.MockK8sMountProvider{
		IsLikelyNotMountPointFunc: func(_ string) (bool, error) { return false, nil },
		UnmountFunc: func(_ string) error {
			return errors.New("unmount failed")
		},
	}

	mounter := services.NewDeviceMounter(
		services.MountConfig{UUID: uuid, MountPoint: dir},
		&mocks.MockDeviceResolver{}, mountProv, &mocks.MockLSBLK{}, noopMounts(),
	)
	if err := mounter.Unmount(context.Background()); err == nil {
		t.Fatal("expected unmount error, got nil")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
