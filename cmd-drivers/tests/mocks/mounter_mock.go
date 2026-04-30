package mocks

import (
	"context"
)

// MockK8sMountProvider is a test double for providers.K8sMountProvider.
// Set each func field before use — leaving it nil will panic, forcing explicit test setup.
type MockK8sMountProvider struct {
	MountFunc                 func(source, target, fstype string, options []string) error
	UnmountFunc               func(target string) error
	IsLikelyNotMountPointFunc func(file string) (bool, error)
}

func (m *MockK8sMountProvider) Mount(source, target, fstype string, options []string) error {
	return m.MountFunc(source, target, fstype, options)
}

func (m *MockK8sMountProvider) Unmount(target string) error {
	return m.UnmountFunc(target)
}

func (m *MockK8sMountProvider) IsLikelyNotMountPoint(file string) (bool, error) {
	return m.IsLikelyNotMountPointFunc(file)
}

// MockDeviceResolver is a test double for providers.DeviceResolver.
// Set ResolveUUIDFunc before use — leaving it nil will panic, forcing explicit test setup.
type MockDeviceResolver struct {
	ResolveUUIDFunc func(ctx context.Context, uuid string) (string, error)
}

func (m *MockDeviceResolver) ResolveUUID(ctx context.Context, uuid string) (string, error) {
	return m.ResolveUUIDFunc(ctx, uuid)
}
