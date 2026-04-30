package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/logger"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/services"
	"github.com/cubbitgg/kubernetes-external-provider/commonlib"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// Config holds all scanner configuration.
type Config struct {
	NodeName     string
	MountBase    string
	FSType       string
	MinSize      uint64
	ScanInterval time.Duration
}

// Scanner discovers local block devices, mounts them, and keeps node
// labels and annotations in sync so the provisioner and scheduler extender
// can react to disk availability.
type Scanner struct {
	config    Config
	client    kubernetes.Interface
	prevUUIDs map[string]struct{}
}

// New returns a new Scanner.
func New(config Config, client kubernetes.Interface) *Scanner {
	return &Scanner{
		config:    config,
		client:    client,
		prevUUIDs: make(map[string]struct{}),
	}
}

// Run starts the periodic scan loop. It blocks until ctx is cancelled.
func (s *Scanner) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)

	// Run immediately on start, then on every tick.
	if err := s.scan(ctx); err != nil {
		log.Error().Err(err).Msg("Initial scan failed")
	}

	ticker := time.NewTicker(s.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.scan(ctx); err != nil {
				log.Error().Err(err).Msg("Scan iteration failed")
			}
		}
	}
}

func (s *Scanner) scan(ctx context.Context) error {
	log := logger.FromContext(ctx)
	lsblk := fsutils.NewLSBLK()
	statfs := providers.NewStatfsProvider()

	// 1. Format any unformatted (raw) disks that are large enough.
	initSvc := services.NewDiskInitializer(
		services.InitConfig{
			FSType:  s.config.FSType,
			MinSize: s.config.MinSize,
			Label:   commonlib.DefaultLabel,
		},
		lsblk,
		providers.NewFormatProvider(),
	)
	if formatted, err := initSvc.Init(ctx); err != nil {
		log.Error().Err(err).Msg("Disk initializer encountered errors")
	} else if len(formatted) > 0 {
		log.Info().Int("count", len(formatted)).Strs("devices", formatted).Msg("Formatted new devices")
	}

	// 2. Mount any formatted-but-unmounted disks.
	scanSvc := services.NewScanner(
		services.ScanConfig{
			DirPrefix: s.config.MountBase,
			FSTypes:   []string{s.config.FSType},
			MinSize:   s.config.MinSize,
		},
		providers.NewMountInfoProvider(s.config.MountBase, []string{s.config.FSType}, s.config.MinSize, statfs),
		statfs,
		lsblk,
	)

	unmounted, err := scanSvc.ScanUnmounted(ctx)
	if err != nil {
		return fmt.Errorf("scan unmounted devices: %w", err)
	}

	for _, dev := range unmounted {
		if dev.UUID == "" || dev.UUID == "N/A" {
			continue
		}
		mntSvc := services.NewDeviceMounter(
			services.MountConfig{
				UUID:         dev.UUID,
				MountPoint:   s.config.MountBase,
				FSType:       s.config.FSType,
				ManagedOnly:  true,
				RequireLabel: commonlib.DefaultLabel,
			},
			providers.NewDeviceResolver(lsblk),
			providers.NewK8sMountProvider(),
			lsblk,
			providers.NewUnfilteredMountInfoProvider(),
		)
		if err := mntSvc.Mount(ctx); err != nil {
			log.Error().Err(err).Str("uuid", dev.UUID).Msg("Failed to mount device")
		} else {
			log.Info().Str("uuid", dev.UUID).Str("mountBase", s.config.MountBase).Msg("Mounted device")
		}
	}

	// 3. Build the current UUID -> DiskEntry map from all mounted disks.
	mounted, err := scanSvc.ScanMounted(ctx)
	if err != nil {
		return fmt.Errorf("scan mounted devices: %w", err)
	}

	diskMap := make(map[string]commonlib.DiskEntry, len(mounted))
	currentUUIDs := make(map[string]struct{}, len(mounted))
	for _, dev := range mounted {
		if dev.UUID == "" || dev.UUID == "N/A" {
			continue
		}
		diskMap[dev.UUID] = commonlib.DiskEntry{
			Path: dev.MountPath,
			Size: dev.TotalSize,
		}
		currentUUIDs[dev.UUID] = struct{}{}
	}

	// 4. Warn about disks that were present last cycle but are now missing.
	for uuid := range s.prevUUIDs {
		if _, ok := currentUUIDs[uuid]; !ok {
			log.Warn().Str("uuid", uuid).Str("node", s.config.NodeName).Msg("Disk UUID previously seen on node is no longer visible")
		}
	}
	s.prevUUIDs = currentUUIDs

	// 5. Update node labels and annotations.
	return s.patchNode(ctx, diskMap, currentUUIDs)
}

// patchNode applies a strategic merge patch that:
//   - Adds label agent.cubbit.io/has-uuid-<UUID>="true" for each current disk
//   - Removes labels for disks that are no longer present
//   - Writes the full UUID->DiskEntry map as JSON into the disk-map annotation
func (s *Scanner) patchNode(ctx context.Context, diskMap map[string]commonlib.DiskEntry, currentUUIDs map[string]struct{}) error {
	diskMapJSON, err := json.Marshal(diskMap)
	if err != nil {
		return fmt.Errorf("marshal disk map: %w", err)
	}

	labels := make(map[string]any, len(currentUUIDs)+len(s.prevUUIDs))
	for uuid := range currentUUIDs {
		labels[commonlib.LabelUUIDPrefix+uuid] = "true"
	}
	// Set stale labels to null so the strategic merge patch removes them.
	for uuid := range s.prevUUIDs {
		if _, ok := currentUUIDs[uuid]; !ok {
			labels[commonlib.LabelUUIDPrefix+uuid] = nil
		}
	}

	patch := map[string]any{
		"metadata": map[string]any{
			"labels": labels,
			"annotations": map[string]string{
				commonlib.AnnotationDiskMap: string(diskMapJSON),
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal node patch: %w", err)
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := s.client.CoreV1().Nodes().Patch(
			ctx, s.config.NodeName,
			types.StrategicMergePatchType,
			patchBytes,
			metav1.PatchOptions{},
		)
		return err
	})
}
