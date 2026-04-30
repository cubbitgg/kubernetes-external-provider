package providers

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
)

// DeviceResolver resolves a filesystem UUID to the block device path (e.g. /dev/sdb1).
type DeviceResolver interface {
	ResolveUUID(ctx context.Context, uuid string) (devicePath string, err error)
}

type realDeviceResolver struct {
	lsblk fsutils.LSBLK
}

// NewDeviceResolver returns a DeviceResolver that first tries the /dev/disk/by-uuid/
// symlink, then falls back to lsblk enumeration.
func NewDeviceResolver(lsblk fsutils.LSBLK) DeviceResolver {
	return &realDeviceResolver{lsblk: lsblk}
}

func (r *realDeviceResolver) ResolveUUID(ctx context.Context, uuid string) (string, error) {
	// Fast path: udev symlink (no subprocess needed).
	symlink := "/dev/disk/by-uuid/" + uuid
	if resolved, err := filepath.EvalSymlinks(symlink); err == nil {
		return resolved, nil
	}

	// Fallback: enumerate all block devices via lsblk and search by UUID/PartUUID.
	devices, err := r.lsblk.GetBlockDevices(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("lsblk enumeration failed: %w", err)
	}
	for _, d := range fsutils.FlattenDevices(devices) {
		if d.UUID == uuid || d.PartUUID == uuid {
			return d.Name, nil
		}
	}

	return "", fmt.Errorf("device with UUID %q not found", uuid)
}
