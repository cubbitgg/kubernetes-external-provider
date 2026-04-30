package providers

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/logger"
)

// FormatOptions holds parameters for formatting a block device.
type FormatOptions struct {
	FSType string
	Label  string // optional; empty means no label flag is passed to mkfs
}

// FormatProvider formats a block device with a given filesystem type.
type FormatProvider interface {
	Format(ctx context.Context, device string, opts FormatOptions) error
}

type realFormatProvider struct{}

// NewFormatProvider returns a FormatProvider that invokes mkfs.<fsType>.
func NewFormatProvider() FormatProvider {
	return &realFormatProvider{}
}

func (p *realFormatProvider) Format(ctx context.Context, device string, opts FormatOptions) error {
	log := logger.FromContext(ctx)
	if !fsutils.IsValidFSType(opts.FSType) {
		return fmt.Errorf("unsupported filesystem type %q", opts.FSType)
	}

	args := []string{device}
	if opts.Label != "" {
		switch opts.FSType {
		case string(fsutils.FSTypeVFAT):
			args = append([]string{"-n", opts.Label}, args...)
		default: // ext4, xfs, ntfs all use -L
			args = append([]string{"-L", opts.Label}, args...)
		}
	}

	cmd := exec.CommandContext(ctx, "mkfs."+opts.FSType, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mkfs.%s %q failed: %w\noutput: %s", opts.FSType, device, err, out)
	}

	syscall.Sync()

	settleCmd := exec.CommandContext(ctx, "udevadm", "settle", "--timeout=5")
	if out, err := settleCmd.CombinedOutput(); err != nil {
		log.Warn().Err(err).Str("device", device).Bytes("output", out).
			Msg("[format] udevadm settle failed after mkfs; lsblk may see stale data briefly")
	}
	return nil
}
