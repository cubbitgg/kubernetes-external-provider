package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/models"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/services"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/tests/mocks"
)

func testCtx() context.Context { return context.Background() }

func defaultConfig() services.ScanConfig {
	return services.ScanConfig{
		DirPrefix: "/mnt",
		FSTypes:   []string{"ext4"},
		MinSize:   10 * 1024 * 1024,
	}
}

// --- ScanMounted ---

func TestUnit_ScanMounted_HappyPath(t *testing.T) {
	scanner := services.NewScanner(
		defaultConfig(),
		&mocks.MockMountInfoProvider{
			GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
				return []models.MountEntry{
					{Source: "/dev/sda1", Mountpoint: "/mnt/data", FSType: "ext4"},
				}, nil
			},
		},
		&mocks.MockStatfsProvider{
			StatfsFunc: func(_ string) (*models.StatfsResult, error) {
				return &models.StatfsResult{TotalSize: 100 << 20, FreeSpace: 60 << 20}, nil
			},
		},
		&mocks.MockLSBLK{
			GetUUIDFunc: func(_ context.Context, _ string) (string, error) {
				return "abc-123", nil
			},
		},
	)

	devices, err := scanner.ScanMounted(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	d := devices[0]
	if d.UUID != "abc-123" {
		t.Errorf("UUID: want abc-123, got %s", d.UUID)
	}
	if d.Device != "/dev/sda1" {
		t.Errorf("Device: want /dev/sda1, got %s", d.Device)
	}
	if d.MountPath != "/mnt/data" {
		t.Errorf("MountPath: want /mnt/data, got %s", d.MountPath)
	}
	if d.Status != models.StatusMounted {
		t.Errorf("Status: want mounted, got %s", d.Status)
	}
	if d.TotalSize != 100<<20 {
		t.Errorf("TotalSize: want %d, got %d", 100<<20, d.TotalSize)
	}
	if d.UsedSpace != 40<<20 {
		t.Errorf("UsedSpace: want %d, got %d", 40<<20, d.UsedSpace)
	}
}

func TestUnit_ScanMounted_StatfsError(t *testing.T) {
	scanner := services.NewScanner(
		defaultConfig(),
		&mocks.MockMountInfoProvider{
			GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
				return []models.MountEntry{
					{Source: "/dev/sda1", Mountpoint: "/mnt/data", FSType: "ext4"},
				}, nil
			},
		},
		&mocks.MockStatfsProvider{
			StatfsFunc: func(_ string) (*models.StatfsResult, error) {
				return nil, errors.New("statfs failed")
			},
		},
		&mocks.MockLSBLK{
			GetUUIDFunc: func(_ context.Context, _ string) (string, error) {
				return "abc-123", nil
			},
		},
	)

	devices, err := scanner.ScanMounted(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device even on statfs failure, got %d", len(devices))
	}
	if devices[0].TotalSize != 0 || devices[0].FreeSpace != 0 || devices[0].UsedSpace != 0 {
		t.Error("expected zero size fields when statfs fails")
	}
}

func TestUnit_ScanMounted_NoUUID(t *testing.T) {
	scanner := services.NewScanner(
		defaultConfig(),
		&mocks.MockMountInfoProvider{
			GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
				return []models.MountEntry{
					{Source: "/dev/sda1", Mountpoint: "/mnt/data", FSType: "ext4"},
				}, nil
			},
		},
		&mocks.MockStatfsProvider{
			StatfsFunc: func(_ string) (*models.StatfsResult, error) {
				return &models.StatfsResult{TotalSize: 100 << 20, FreeSpace: 60 << 20}, nil
			},
		},
		&mocks.MockLSBLK{
			GetUUIDFunc: func(_ context.Context, _ string) (string, error) {
				return "", errors.New("uuid not found")
			},
		},
	)

	devices, err := scanner.ScanMounted(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if devices[0].UUID != "N/A" {
		t.Errorf("UUID: want N/A, got %s", devices[0].UUID)
	}
}

func TestUnit_ScanMounted_ProviderError(t *testing.T) {
	scanner := services.NewScanner(
		defaultConfig(),
		&mocks.MockMountInfoProvider{
			GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
				return nil, errors.New("mountinfo unavailable")
			},
		},
		&mocks.MockStatfsProvider{},
		&mocks.MockLSBLK{},
	)

	_, err := scanner.ScanMounted(testCtx())
	if err == nil {
		t.Fatal("expected error from failed provider, got nil")
	}
}

// --- ScanUnmounted ---

func TestUnit_ScanUnmounted_HappyPath(t *testing.T) {
	scanner := services.NewScanner(
		services.ScanConfig{FSTypes: []string{"ext4"}, MinSize: 1 << 20},
		&mocks.MockMountInfoProvider{},
		&mocks.MockStatfsProvider{},
		&mocks.MockLSBLK{
			GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
				return []fsutils.BlockDevice{
					{Name: "/dev/sdb", Type: "disk", FSType: "ext4", UUID: "disk-uuid", Size: 100 << 20},
					{Name: "/dev/sdc1", Type: "part", FSType: "ext4", UUID: "part-uuid", Size: 50 << 20},
				}, nil
			},
		},
	)

	devices, err := scanner.ScanUnmounted(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}

	disk := devices[0]
	if disk.UUID != "disk-uuid" {
		t.Errorf("disk UUID: want disk-uuid, got %s", disk.UUID)
	}
	if disk.Status != models.StatusNotPartitioned {
		t.Errorf("disk Status: want not partitioned, got %s", disk.Status)
	}

	part := devices[1]
	if part.Status != models.StatusPartitioned {
		t.Errorf("part Status: want partitioned, got %s", part.Status)
	}
}

func TestUnit_ScanUnmounted_SkipDiskWithChildren(t *testing.T) {
	scanner := services.NewScanner(
		services.ScanConfig{FSTypes: []string{"ext4"}, MinSize: 0},
		&mocks.MockMountInfoProvider{},
		&mocks.MockStatfsProvider{},
		&mocks.MockLSBLK{
			GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
				return []fsutils.BlockDevice{
					{
						Name: "/dev/sda", Type: "disk", Size: 200 << 20,
						Children: []fsutils.BlockDevice{
							{Name: "/dev/sda1", Type: "part", Size: 100 << 20},
						},
					},
				}, nil
			},
		},
	)

	devices, err := scanner.ScanUnmounted(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected disk with children to be skipped, got %d devices", len(devices))
	}
}

func TestUnit_ScanUnmounted_SkipSmallDevice(t *testing.T) {
	scanner := services.NewScanner(
		services.ScanConfig{FSTypes: []string{"ext4"}, MinSize: 50 << 20},
		&mocks.MockMountInfoProvider{},
		&mocks.MockStatfsProvider{},
		&mocks.MockLSBLK{
			GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
				return []fsutils.BlockDevice{
					{Name: "/dev/sdb", Type: "disk", FSType: "ext4", Size: 10 << 20},
				}, nil
			},
		},
	)

	devices, err := scanner.ScanUnmounted(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected small device to be skipped, got %d devices", len(devices))
	}
}

func TestUnit_ScanUnmounted_UUIDFallback(t *testing.T) {
	scanner := services.NewScanner(
		services.ScanConfig{FSTypes: []string{"ext4"}, MinSize: 0},
		&mocks.MockMountInfoProvider{},
		&mocks.MockStatfsProvider{},
		&mocks.MockLSBLK{
			GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
				return []fsutils.BlockDevice{
					{Name: "/dev/sda", Type: "disk", UUID: "", PartUUID: "part-uuid-fallback", Size: 100 << 20},
					{Name: "/dev/sdb", Type: "disk", UUID: "", PartUUID: "", Size: 100 << 20},
				}, nil
			},
		},
	)

	devices, err := scanner.ScanUnmounted(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if devices[0].UUID != "part-uuid-fallback" {
		t.Errorf("expected PartUUID fallback, got %s", devices[0].UUID)
	}
	if devices[1].UUID != "N/A" {
		t.Errorf("expected N/A when both UUID and PartUUID empty, got %s", devices[1].UUID)
	}
}

// --- ScanAll ---

func TestUnit_ScanAll_MergeAndSort(t *testing.T) {
	scanner := services.NewScanner(
		services.ScanConfig{FSTypes: []string{"ext4"}, MinSize: 0},
		&mocks.MockMountInfoProvider{
			GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
				return []models.MountEntry{
					{Source: "/dev/sda1", Mountpoint: "/mnt/a", FSType: "ext4"},
				}, nil
			},
		},
		&mocks.MockStatfsProvider{
			StatfsFunc: func(_ string) (*models.StatfsResult, error) {
				return &models.StatfsResult{TotalSize: 100 << 20, FreeSpace: 50 << 20}, nil
			},
		},
		&mocks.MockLSBLK{
			GetUUIDFunc: func(_ context.Context, _ string) (string, error) {
				return "zzz-mounted", nil
			},
			GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
				return []fsutils.BlockDevice{
					{Name: "/dev/sdb", Type: "disk", UUID: "aaa-unmounted", Size: 100 << 20},
				}, nil
			},
		},
	)

	devices, err := scanner.ScanAll(testCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
	// sorted by UUID: "aaa-unmounted" < "zzz-mounted"
	if devices[0].UUID != "aaa-unmounted" {
		t.Errorf("first device should be aaa-unmounted, got %s", devices[0].UUID)
	}
	if devices[1].UUID != "zzz-mounted" {
		t.Errorf("second device should be zzz-mounted, got %s", devices[1].UUID)
	}
}

func TestUnit_ScanAll_MountedError(t *testing.T) {
	scanner := services.NewScanner(
		defaultConfig(),
		&mocks.MockMountInfoProvider{
			GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
				return nil, errors.New("read failed")
			},
		},
		&mocks.MockStatfsProvider{},
		&mocks.MockLSBLK{},
	)

	_, err := scanner.ScanAll(testCtx())
	if err == nil {
		t.Fatal("expected error from ScanAll when ScanMounted fails, got nil")
	}
}

func TestUnit_ScanAll_UnmountedError(t *testing.T) {
	scanner := services.NewScanner(
		defaultConfig(),
		&mocks.MockMountInfoProvider{
			GetMountsFunc: func(_ context.Context) ([]models.MountEntry, error) {
				return []models.MountEntry{}, nil
			},
		},
		&mocks.MockStatfsProvider{},
		&mocks.MockLSBLK{
			GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
				return nil, errors.New("lsblk failed")
			},
		},
	)

	_, err := scanner.ScanAll(testCtx())
	if err == nil {
		t.Fatal("expected error from ScanAll when ScanUnmounted fails, got nil")
	}
}
