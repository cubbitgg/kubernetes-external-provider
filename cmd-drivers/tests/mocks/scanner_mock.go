package mocks

import (
	"context"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/models"
)

// MockMountInfoProvider is a test double for services.MountInfoProvider.
// Set GetMountsFunc before use — leaving it nil will panic, forcing explicit test setup.
type MockMountInfoProvider struct {
	GetMountsFunc func(ctx context.Context) ([]models.MountEntry, error)
}

func (m *MockMountInfoProvider) GetMounts(ctx context.Context) ([]models.MountEntry, error) {
	return m.GetMountsFunc(ctx)
}

// MockStatfsProvider is a test double for services.StatfsProvider.
// Set StatfsFunc before use — leaving it nil will panic, forcing explicit test setup.
type MockStatfsProvider struct {
	StatfsFunc func(path string) (*models.StatfsResult, error)
}

func (m *MockStatfsProvider) Statfs(path string) (*models.StatfsResult, error) {
	return m.StatfsFunc(path)
}

// MockLSBLK is a test double for fsutils.LSBLK.
// Set only the func fields your test actually exercises — unused fields stay nil.
type MockLSBLK struct {
	GetBlockDevicesFunc func(ctx context.Context, filter fsutils.FilterFunc) ([]fsutils.BlockDevice, error)
	GetBlockDeviceFunc  func(ctx context.Context, device string) (*fsutils.BlockDevice, error)
	GetUUIDFunc         func(ctx context.Context, device string) (string, error)
}

func (m *MockLSBLK) GetBlockDevices(ctx context.Context, filter fsutils.FilterFunc) ([]fsutils.BlockDevice, error) {
	return m.GetBlockDevicesFunc(ctx, filter)
}

func (m *MockLSBLK) GetBlockDevice(ctx context.Context, device string) (*fsutils.BlockDevice, error) {
	return m.GetBlockDeviceFunc(ctx, device)
}

func (m *MockLSBLK) GetUUID(ctx context.Context, device string) (string, error) {
	return m.GetUUIDFunc(ctx, device)
}
