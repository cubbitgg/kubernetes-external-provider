package loopdev

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
)

// Device holds the sparse file path and the loop device path (e.g. "/dev/loop5").
type Device struct {
	FilePath   string // path to the sparse backing file
	DevicePath string // e.g. "/dev/loop5"
}

// RequireRoot skips the test if not running as root.
func RequireRoot(t *testing.T) {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("test requires root (loop device creation needs CAP_SYS_ADMIN)")
	}
}

// Create creates a sparse file of the given size and attaches it as a loop device.
// It registers t.Cleanup() to detach the loop device and remove the sparse file.
// Returns a Device with the file and device paths.
func Create(t *testing.T, size int64) Device {
	t.Helper()
	t.Logf("Creating loop device with size %d bytes", size)

	f, err := os.CreateTemp("", "loopdev-e2e-*.img")
	if err != nil {
		t.Fatalf("loopdev: create temp file: %v", err)
	}
	filePath := f.Name()

	if err := f.Truncate(size); err != nil {
		f.Close()
		os.Remove(filePath)
		t.Fatalf("loopdev: truncate sparse file: %v", err)
	}
	f.Close()

	out, err := exec.Command("losetup", "--find", "--show", filePath).CombinedOutput()
	if err != nil {
		os.Remove(filePath)
		t.Fatalf("loopdev: losetup --find --show: %v\noutput: %s", err, out)
	}
	devicePath := strings.TrimSpace(string(out))
	t.Logf("Created loop device %s for file %s", devicePath, filePath)

	t.Cleanup(func() {
		t.Logf("Cleaning up: detaching loop device %s and removing file %s", devicePath, filePath)
		exec.Command("losetup", "--detach", devicePath).Run() //nolint:errcheck
		os.Remove(filePath)
	})

	return Device{
		FilePath:   filePath,
		DevicePath: devicePath,
	}
}

// Format formats the device with the given filesystem type using the real FormatProvider.
// It calls udevadm settle after formatting to ensure the kernel/udev cache is updated.
func Format(t *testing.T, dev Device, fsType string) {
	t.Helper()
	if err := providers.NewFormatProvider().Format(context.Background(), dev.DevicePath, providers.FormatOptions{FSType: fsType}); err != nil {
		t.Fatalf("loopdev: format %s with %s: %v", dev.DevicePath, fsType, err)
	}
	exec.Command("udevadm", "settle").Run() //nolint:errcheck
	t.Logf("Formatted %s as %s", dev.DevicePath, fsType)
}

// UUID returns the filesystem UUID of the device, waiting up to 5 seconds for
// udev to populate it. Fatally fails the test if the UUID cannot be determined.
func UUID(t *testing.T, dev Device) string {
	t.Helper()
	exec.Command("udevadm", "settle").Run() //nolint:errcheck
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("lsblk", "--nodeps", "--noheadings", "--output", "UUID", dev.DevicePath).CombinedOutput()
		if err != nil {
			t.Fatalf("loopdev: lsblk UUID for %s: %v\noutput: %s", dev.DevicePath, err, out)
		}
		if u := strings.TrimSpace(string(out)); u != "" {
			t.Logf("UUID for %s: %s", dev.DevicePath, u)
			return u
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("loopdev: timed out waiting for UUID on %s", dev.DevicePath)
	return ""
}

// Mount mounts the device at the given mount point, creating the directory if needed.
// It registers t.Cleanup() to unmount before the loop device is detached.
func Mount(t *testing.T, dev Device, mountPoint string) {
	t.Helper()
	if err := os.MkdirAll(mountPoint, 0750); err != nil {
		t.Fatalf("loopdev: mkdir %s: %v", mountPoint, err)
	}
	if out, err := exec.Command("mount", dev.DevicePath, mountPoint).CombinedOutput(); err != nil {
		t.Fatalf("loopdev: mount %s at %s: %v\noutput: %s", dev.DevicePath, mountPoint, err, out)
	}
	t.Logf("Mounted %s at %s", dev.DevicePath, mountPoint)
	t.Cleanup(func() {
		t.Logf("Cleaning up: unmounting %s", mountPoint)
		exec.Command("umount", mountPoint).Run() //nolint:errcheck
	})
}

// FSType returns the filesystem type reported by lsblk for the device, waiting
// up to 5 seconds for udev to populate it. Returns an empty string if none.
func FSType(t *testing.T, dev Device) string {
	t.Helper()
	exec.Command("udevadm", "settle").Run() //nolint:errcheck
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("lsblk", "--nodeps", "--noheadings", "--output", "FSTYPE", dev.DevicePath).CombinedOutput()
		if err != nil {
			t.Fatalf("loopdev: lsblk FSTYPE for %s: %v\noutput: %s", dev.DevicePath, err, out)
		}
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ""
}

// MountPoint returns the current mount point of the device as reported by lsblk,
// or an empty string if the device is not mounted.
func MountPoint(t *testing.T, dev Device) string {
	t.Helper()
	out, err := exec.Command("lsblk", "--nodeps", "--noheadings", "--output", "MOUNTPOINT", dev.DevicePath).CombinedOutput()
	if err != nil {
		t.Fatalf("loopdev: lsblk MOUNTPOINT for %s: %v\noutput: %s", dev.DevicePath, err, out)
	}
	return strings.TrimSpace(string(out))
}

// CreateN creates n loop devices each of the given size.
func CreateN(t *testing.T, n int, size int64) []Device {
	t.Helper()
	devices := make([]Device, n)
	for i := range devices {
		devices[i] = Create(t, size)
	}
	return devices
}
