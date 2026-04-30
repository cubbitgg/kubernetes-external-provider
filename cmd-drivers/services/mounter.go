package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/logger"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
)

// MountConfig holds parameters for a mount or unmount operation.
type MountConfig struct {
	UUID         string   // filesystem UUID of the device to mount
	MountPoint   string   // base directory; the device will be mounted at <MountPoint>/<UUID>
	FSType       string   // filesystem type (e.g. "ext4"); auto-detected via lsblk if empty
	Options      []string // mount options (e.g. ["noatime", "discard"])
	ManagedOnly  bool     // if true, enforce RequireLabel
	RequireLabel string   // device must carry this filesystem label when ManagedOnly is true
}

// DeviceMounter mounts or unmounts a block device identified by UUID.
type DeviceMounter interface {
	Mount(ctx context.Context) error
	Unmount(ctx context.Context) error
}

type deviceMounter struct {
	config   MountConfig
	resolver providers.DeviceResolver
	mount    providers.K8sMountProvider
	lsblk    fsutils.LSBLK
	mounts   providers.MountInfoProvider
}

// NewDeviceMounter creates a DeviceMounter with injected dependencies.
func NewDeviceMounter(
	config MountConfig,
	resolver providers.DeviceResolver,
	mount providers.K8sMountProvider,
	lsblk fsutils.LSBLK,
	mounts providers.MountInfoProvider,
) DeviceMounter {
	return &deviceMounter{
		config:   config,
		resolver: resolver,
		mount:    mount,
		lsblk:    lsblk,
		mounts:   mounts,
	}
}

// Mount resolves the UUID to a device path, creates the target directory, and mounts the device.
// The operation is idempotent: if the target is already mounted, Mount returns nil.
// It always checks that the resolved device is not on the root disk (fail-open if detection fails).
// When ManagedOnly is set it also verifies the device's filesystem label matches RequireLabel.
func (m *deviceMounter) Mount(ctx context.Context) error {
	log := logger.FromContext(ctx)

	devicePath, err := m.resolver.ResolveUUID(ctx, m.config.UUID)
	if err != nil {
		return fmt.Errorf("resolve UUID %q: %w", m.config.UUID, err)
	}

	// Root-disk guardrail — always-on.
	if err := m.checkNotRootDisk(ctx, devicePath); err != nil {
		return err
	}

	// Fetch device info when needed for label check or fs-type auto-detection.
	var devInfo *fsutils.BlockDevice
	if m.config.ManagedOnly || m.config.FSType == "" {
		di, err := m.lsblk.GetBlockDevice(ctx, devicePath)
		if err != nil {
			return fmt.Errorf("read device info for %q: %w", devicePath, err)
		}
		devInfo = di
	}

	// Label filter — opt-in via ManagedOnly.
	if m.config.ManagedOnly {
		if devInfo.Label != m.config.RequireLabel {
			return fmt.Errorf("refusing to mount %q: label %q does not match required %q",
				devicePath, devInfo.Label, m.config.RequireLabel)
		}
	}

	target := filepath.Join(m.config.MountPoint, m.config.UUID)

	if err := os.MkdirAll(target, 0750); err != nil {
		return fmt.Errorf("create mount target %q: %w", target, err)
	}

	notMounted, err := m.mount.IsLikelyNotMountPoint(target)
	if err != nil {
		return fmt.Errorf("check mount point %q: %w", target, err)
	}
	if !notMounted {
		log.Info().Str("target", target).Msg("[mounter] Already mounted, skipping")
		return nil
	}

	fsType := m.config.FSType
	if fsType == "" {
		fsType = devInfo.FSType
		if fsType == "" {
			return fmt.Errorf("cannot determine filesystem type for %q: device has no filesystem", devicePath)
		}
	}

	log.Info().
		Str("device", devicePath).
		Str("target", target).
		Str("fstype", fsType).
		Strs("options", m.config.Options).
		Msg("[mounter] Mounting device")

	if err := m.mount.Mount(devicePath, target, fsType, m.config.Options); err != nil {
		return err
	}

	log.Info().Str("device", devicePath).Str("target", target).Msg("[mounter] Mount successful")
	return nil
}

// checkNotRootDisk returns an error if devicePath belongs to the disk that hosts "/".
// Fails open (returns nil) when the root disk cannot be determined.
func (m *deviceMounter) checkNotRootDisk(ctx context.Context, devicePath string) error {
	log := logger.FromContext(ctx)

	entries, err := m.mounts.GetMounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("[mounter] failed to read mounts for root-disk check; skipping guardrail")
		return nil
	}

	rootSource := ""
	for _, e := range entries {
		if e.Mountpoint == "/" {
			rootSource = e.Source
			break
		}
	}
	if rootSource == "" {
		log.Warn().Msg("[mounter] could not detect root filesystem source; skipping root-disk guardrail")
		return nil
	}

	allDevices, err := m.lsblk.GetBlockDevices(ctx, nil)
	if err != nil {
		log.Warn().Err(err).Msg("[mounter] failed to enumerate devices for root-disk check; skipping guardrail")
		return nil
	}

	excluded := fsutils.FindRootDiskDescendants(allDevices, rootSource)
	if _, isExcluded := excluded[devicePath]; isExcluded {
		return fmt.Errorf("refusing to mount %q: device is on the root disk (%s)", devicePath, rootSource)
	}
	return nil
}

// Unmount unmounts the device at <MountPoint>/<UUID> and removes the empty directory.
// The operation is idempotent: if the target is not mounted, Unmount returns nil.
func (m *deviceMounter) Unmount(ctx context.Context) error {
	log := logger.FromContext(ctx)

	target := filepath.Join(m.config.MountPoint, m.config.UUID)

	if _, err := os.Stat(target); os.IsNotExist(err) {
		log.Info().Str("target", target).Msg("[mounter] Mount point does not exist, nothing to unmount")
		return nil
	}

	notMounted, err := m.mount.IsLikelyNotMountPoint(target)
	if err != nil {
		return fmt.Errorf("check mount point %q: %w", target, err)
	}
	if notMounted {
		log.Info().Str("target", target).Msg("[mounter] Not mounted, skipping")
		return nil
	}

	log.Info().Str("target", target).Msg("[mounter] Unmounting device")

	if err := m.mount.Unmount(target); err != nil {
		return err
	}

	// Best-effort: remove the now-empty directory.
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		log.Warn().Str("target", target).Err(err).Msg("[mounter] Could not remove mount directory after unmount")
	}

	log.Info().Str("target", target).Msg("[mounter] Unmount successful")
	return nil
}
