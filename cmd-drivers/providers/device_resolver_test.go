package providers_test

import (
	"context"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/tests/mocks"
)

func TestUnit_ResolveUUID_LsblkFallback(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{
				{Name: "/dev/sdb1", UUID: uuid},
			}, nil
		},
	}
	resolver := providers.NewDeviceResolver(lsblk)
	got, err := resolver.ResolveUUID(context.Background(), uuid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/dev/sdb1" {
		t.Errorf("expected /dev/sdb1, got %q", got)
	}
}

func TestUnit_ResolveUUID_NotFound(t *testing.T) {
	lsblk := &mocks.MockLSBLK{
		GetBlockDevicesFunc: func(_ context.Context, _ fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
			return []fsutils.BlockDevice{}, nil
		},
	}
	resolver := providers.NewDeviceResolver(lsblk)
	_, err := resolver.ResolveUUID(context.Background(), "550e8400-e29b-41d4-a716-446655440000")
	if err == nil {
		t.Fatal("expected error for unknown UUID, got nil")
	}
}
