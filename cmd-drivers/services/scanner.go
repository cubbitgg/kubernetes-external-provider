package services

import (
	"context"
	"fmt"
	"sort"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/logger"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/models"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
)

// ScanConfig holds filtering parameters for device scanning.
type ScanConfig struct {
	DirPrefix string   // filter mounted devices under this path (e.g. "/mnt/cubbit")
	FSTypes   []string // filesystem types to include (e.g. ["ext4"])
	MinSize   uint64   // minimum device size in bytes
}

// DeviceScanner returns lists of block devices, both mounted and unmounted.
type DeviceScanner interface {
	ScanAll(ctx context.Context) ([]models.DeviceInfo, error)
	ScanMounted(ctx context.Context) ([]models.DeviceInfo, error)
	ScanUnmounted(ctx context.Context) ([]models.DeviceInfo, error)
}

type scanner struct {
	config ScanConfig
	mounts providers.MountInfoProvider
	statfs providers.StatfsProvider
	lsblk  fsutils.LSBLK
}

// NewScanner creates a DeviceScanner with the given dependencies.
func NewScanner(config ScanConfig, mounts providers.MountInfoProvider, statfs providers.StatfsProvider, lsblk fsutils.LSBLK) DeviceScanner {
	return &scanner{
		config: config,
		mounts: mounts,
		statfs: statfs,
		lsblk:  lsblk,
	}
}

// ScanAll returns all devices (mounted + unmounted), sorted by UUID.
func (s *scanner) ScanAll(ctx context.Context) ([]models.DeviceInfo, error) {
	mounted, err := s.ScanMounted(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning mounted devices: %w", err)
	}

	unmounted, err := s.ScanUnmounted(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning unmounted devices: %w", err)
	}

	all := make([]models.DeviceInfo, 0, len(mounted)+len(unmounted))
	all = append(all, mounted...)
	all = append(all, unmounted...)
	sort.Slice(all, func(i, j int) bool {
		return all[i].UUID < all[j].UUID
	})
	return all, nil
}

// ScanMounted returns mounted devices that match the configured filters.
func (s *scanner) ScanMounted(ctx context.Context) ([]models.DeviceInfo, error) {
	log := logger.FromContext(ctx)

	entries, err := s.mounts.GetMounts(ctx)
	if err != nil {
		log.Error().Err(err).Msg("[scanner] Failed to read mounts")
		return []models.DeviceInfo{}, err
	}

	devices := make([]models.DeviceInfo, 0, len(entries))
	for _, entry := range entries {
		log.Debug().
			Str("device", entry.Source).
			Str("mountpoint", entry.Mountpoint).
			Str("fstype", entry.FSType).
			Msg("[scanner] Processing mount")

		info := models.DeviceInfo{
			UUID:      s.getUUID(ctx, entry.Source),
			Device:    entry.Source,
			MountPath: entry.Mountpoint,
			FSType:    entry.FSType,
			Status:    models.StatusMounted,
		}

		if stats, err := s.statfs.Statfs(entry.Mountpoint); err == nil {
			info.TotalSize = stats.TotalSize
			info.FreeSpace = stats.FreeSpace
			info.UsedSpace = stats.TotalSize - stats.FreeSpace
		} else {
			log.Warn().Err(err).Str("mountpoint", entry.Mountpoint).Msg("[scanner] Failed to get disk stats")
		}

		devices = append(devices, info)
	}

	log.Info().Int("count", len(devices)).Msg("[scanner] Retrieved mounted devices")
	return devices, nil
}

// ScanUnmounted returns unmounted block devices that match the configured filters.
func (s *scanner) ScanUnmounted(ctx context.Context) ([]models.DeviceInfo, error) {
	log := logger.FromContext(ctx)

	filter := fsutils.And(
		func(bd *fsutils.BlockDevice) (name string, skip bool, stop bool) {
			return "MountpointEmpty", bd.Mountpoint != "", false
		},
		fsutils.FSTypeFilter(s.config.FSTypes...),
		fsutils.TypeFilter("disk", "part", "loop"),
	)

	blockDevices, err := s.lsblk.GetBlockDevices(ctx, filter)
	if err != nil {
		log.Error().Err(err).Msg("[scanner] Failed to get block devices")
		return []models.DeviceInfo{}, err
	}

	var devices []models.DeviceInfo
	for _, bd := range blockDevices {
		if bd.Type == "disk" && len(bd.Children) > 0 {
			log.Debug().Str("device", bd.Name).Int("children", len(bd.Children)).Msg("[scanner] Skipping disk with partitions")
			continue
		}

		totalSize := uint64(bd.Size)
		if totalSize < s.config.MinSize {
			log.Debug().Str("device", bd.Name).Uint64("size", totalSize).Msg("[scanner] Skipping device smaller than minimum size")
			continue
		}

		uuid := bd.UUID
		if uuid == "" {
			uuid = bd.PartUUID
		}
		if uuid == "" {
			uuid = "N/A"
		}

		status := models.StatusNotPartitioned
		if bd.Type == "part" {
			status = models.StatusPartitioned
		}

		devices = append(devices, models.DeviceInfo{
			UUID:      uuid,
			Device:    bd.Name,
			FSType:    bd.FSType,
			Status:    status,
			TotalSize: totalSize,
		})

		log.Debug().Str("device", bd.Name).Str("status", string(status)).Msg("[scanner] Added unmounted device")
	}

	log.Info().Int("count", len(devices)).Msg("[scanner] Retrieved unmounted devices")
	return devices, nil
}

func (s *scanner) getUUID(ctx context.Context, device string) string {
	log := logger.FromContext(ctx)
	uuid, err := s.lsblk.GetUUID(ctx, device)
	if err != nil || uuid == "" {
		log.Debug().Err(err).Str("device", device).Msg("[scanner] No UUID found for device")
		return "N/A"
	}
	return uuid
}
