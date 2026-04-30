package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/services"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/tests/mocks"
)

func TestUnit_Init_FormatsUnformattedDisk(t *testing.T) {
	formatted := ""
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{Name: "/dev/sdb", Type: "disk", Size: 100 * 1024 * 1024, FSType: "", UUID: ""},
			}, nil
		},
	}
	format := &mocks.MockFormatProvider{
		FormatFunc: func(_ context.Context, device string, opts providers.FormatOptions) error {
			formatted = device
			return nil
		},
	}

	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 50 * 1024 * 1024}, lsblk, format)
	got, err := init.Init(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "/dev/sdb" {
		t.Errorf("expected [/dev/sdb], got %v", got)
	}
	if formatted != "/dev/sdb" {
		t.Errorf("expected Format called with /dev/sdb, got %q", formatted)
	}
}

func TestUnit_Init_SkipsAlreadyFormatted(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{Name: "/dev/sdb", Type: "disk", Size: 100 * 1024 * 1024, FSType: "ext4", UUID: "abc-123"},
			}, nil
		},
	}
	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 0}, lsblk, &mocks.MockFormatProvider{})
	got, err := init.Init(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no devices formatted, got %v", got)
	}
}

func TestUnit_Init_SkipsDiskWithPartitions(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{
					Name: "/dev/sdb", Type: "disk", Size: 100 * 1024 * 1024,
					Children: []fsutils.BlockDevice{
						{Name: "/dev/sdb1", Type: "part"},
					},
				},
			}, nil
		},
	}
	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 0}, lsblk, &mocks.MockFormatProvider{})
	got, err := init.Init(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no devices formatted, got %v", got)
	}
}

func TestUnit_Init_SkipsBelowMinSize(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{Name: "/dev/sdb", Type: "disk", Size: 10 * 1024 * 1024}, // 10MB
			}, nil
		},
	}
	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 50 * 1024 * 1024}, lsblk, &mocks.MockFormatProvider{})
	got, err := init.Init(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no devices formatted, got %v", got)
	}
}

func TestUnit_Init_DryRunDoesNotFormat(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{Name: "/dev/sdb", Type: "disk", Size: 100 * 1024 * 1024},
			}, nil
		},
	}
	format := &mocks.MockFormatProvider{} // FormatFunc intentionally nil — panics if called

	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 0, DryRun: true}, lsblk, format)
	got, err := init.Init(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "/dev/sdb" {
		t.Errorf("expected [/dev/sdb] in dry-run result, got %v", got)
	}
}

func TestUnit_Init_FormatError(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{Name: "/dev/sdb", Type: "disk", Size: 100 * 1024 * 1024},
			}, nil
		},
	}
	format := &mocks.MockFormatProvider{
		FormatFunc: func(_ context.Context, _ string, _ providers.FormatOptions) error {
			return errors.New("mkfs failed")
		},
	}
	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 0}, lsblk, format)
	_, err := init.Init(context.Background())
	if err == nil {
		t.Fatal("expected format error, got nil")
	}
}

func TestUnit_Init_LsblkError(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return nil, errors.New("lsblk failed")
		},
	}
	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 0}, lsblk, &mocks.MockFormatProvider{})
	_, err := init.Init(context.Background())
	if err == nil {
		t.Fatal("expected lsblk error, got nil")
	}
}

func TestUnit_Init_NoDevicesFound(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{}, nil
		},
	}
	init := services.NewDiskInitializer(services.InitConfig{FSType: "ext4", MinSize: 0}, lsblk, &mocks.MockFormatProvider{})
	got, err := init.Init(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestUnit_Init_PassesLabelToFormat(t *testing.T) {
	var capturedOpts providers.FormatOptions
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{Name: "/dev/sdb", Type: "disk", Size: 100 * 1024 * 1024},
			}, nil
		},
	}
	format := &mocks.MockFormatProvider{
		FormatFunc: func(_ context.Context, _ string, opts providers.FormatOptions) error {
			capturedOpts = opts
			return nil
		},
	}

	init := services.NewDiskInitializer(
		services.InitConfig{FSType: "ext4", Label: "CUBBIT", MinSize: 0},
		lsblk, format,
	)
	if _, err := init.Init(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedOpts.Label != "CUBBIT" {
		t.Errorf("expected label CUBBIT passed to Format, got %q", capturedOpts.Label)
	}
	if capturedOpts.FSType != "ext4" {
		t.Errorf("expected fstype ext4 passed to Format, got %q", capturedOpts.FSType)
	}
}
