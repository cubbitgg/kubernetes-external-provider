package providers

import (
	"context"
	"fmt"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/logger"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/models"
	"github.com/moby/sys/mountinfo"
)

// MountInfoProvider abstracts reading /proc/self/mountinfo.
type MountInfoProvider interface {
	GetMounts(ctx context.Context) ([]models.MountEntry, error)
}

type realMountInfoProvider struct {
	dirPrefix string
	fsTypes   []string
	minSize   uint64
	statfs    StatfsProvider
}

// NewMountInfoProvider returns a MountInfoProvider that filters mounts by
// dirPrefix, fsTypes, and minSize using mountinfo filter composition.
func NewMountInfoProvider(dirPrefix string, fsTypes []string, minSize uint64, statfs StatfsProvider) MountInfoProvider {
	return &realMountInfoProvider{
		dirPrefix: dirPrefix,
		fsTypes:   fsTypes,
		minSize:   minSize,
		statfs:    statfs,
	}
}

func (p *realMountInfoProvider) GetMounts(ctx context.Context) ([]models.MountEntry, error) {
	log := logger.FromContext(ctx)

	filter := and(
		mountinfo.PrefixFilter(p.dirPrefix),
		mountinfo.FSTypeFilter(p.fsTypes...),
		createSizeFilter(p.statfs, p.minSize),
	)

	log.Debug().Msg("Getting mounts from system")
	mounts, err := mountinfo.GetMounts(filter)
	if err != nil {
		return nil, fmt.Errorf("reading mounts: %w", err)
	}

	entries := make([]models.MountEntry, 0, len(mounts))
	for _, m := range mounts {
		entries = append(entries, models.MountEntry{
			Source:     m.Source,
			Mountpoint: m.Mountpoint,
			FSType:     m.FSType,
		})
	}

	log.Debug().Int("count", len(entries)).Msg("Got filtered mounts")
	return entries, nil
}

// and combines multiple mountinfo.FilterFunc with AND logic.
// All filters must pass for the mount to be included.
func and(filters ...mountinfo.FilterFunc) mountinfo.FilterFunc {
	return func(info *mountinfo.Info) (skip bool, stop bool) {
		for _, filter := range filters {
			skip, stop = filter(info)
			if skip || stop {
				return skip, stop
			}
		}
		return false, false
	}
}

// NewUnfilteredMountInfoProvider returns a MountInfoProvider that returns all
// mounts with no filtering — used internally by the mounter for root-disk detection.
func NewUnfilteredMountInfoProvider() MountInfoProvider {
	return &unfilteredMountInfoProvider{}
}

type unfilteredMountInfoProvider struct{}

func (p *unfilteredMountInfoProvider) GetMounts(ctx context.Context) ([]models.MountEntry, error) {
	log := logger.FromContext(ctx)
	mounts, err := mountinfo.GetMounts(nil)
	if err != nil {
		return nil, fmt.Errorf("reading mounts: %w", err)
	}
	entries := make([]models.MountEntry, 0, len(mounts))
	for _, m := range mounts {
		entries = append(entries, models.MountEntry{
			Source:     m.Source,
			Mountpoint: m.Mountpoint,
			FSType:     m.FSType,
		})
	}
	log.Debug().Int("count", len(entries)).Msg("Got all system mounts (unfiltered)")
	return entries, nil
}

// createSizeFilter returns a mountinfo.FilterFunc that skips mount points
// whose total size is smaller than minSize.
func createSizeFilter(statfs StatfsProvider, minSize uint64) mountinfo.FilterFunc {
	return func(info *mountinfo.Info) (skip bool, stop bool) {
		result, err := statfs.Statfs(info.Mountpoint)
		if err != nil {
			return true, false
		}
		if result.TotalSize < minSize {
			return true, false
		}
		return false, false
	}
}
