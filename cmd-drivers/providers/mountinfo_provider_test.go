package providers_test

import (
	"context"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
)

// TestIntegration_MountInfoProvider_GetMounts verifies that the provider reads
// real mount entries from /proc/self/mountinfo and converts them correctly.
// Uses a broad set of common filesystem types to be resilient across environments.
// minSize=0 ensures no mounts are excluded by size.
func TestIntegration_MountInfoProvider_GetMounts(t *testing.T) {
	// Broad fsType list covers bare metal, VMs, containers (overlay/tmpfs/ext4/xfs)
	commonFSTypes := []string{
		"ext4", "xfs", "btrfs", "overlay", "tmpfs",
		"devtmpfs", "sysfs", "proc", "squashfs",
	}

	statfs := providers.NewStatfsProvider()
	p := providers.NewMountInfoProvider("/", commonFSTypes, 0, statfs)

	entries, err := p.GetMounts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one mount entry, got none (check that the test environment has a standard Linux mount table)")
	}

	for _, e := range entries {
		if e.Mountpoint == "" {
			t.Errorf("entry has empty Mountpoint: %+v", e)
		}
		if e.FSType == "" {
			t.Errorf("entry has empty FSType: %+v", e)
		}
	}
}
