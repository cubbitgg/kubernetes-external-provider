package models

// DeviceStatus represents the operational state of a block device.
type DeviceStatus string

const (
	StatusMounted        DeviceStatus = "mounted"
	StatusPartitioned    DeviceStatus = "partitioned"
	StatusNotPartitioned DeviceStatus = "not partitioned"
)

// DeviceInfo describes a block device and its current state.
type DeviceInfo struct {
	UUID      string
	Device    string
	MountPath string
	FSType    string
	Status    DeviceStatus
	TotalSize uint64
	FreeSpace uint64
	UsedSpace uint64
}

// MountEntry is a decoupled representation of a mount point,
// independent of moby/sys/mountinfo internals.
type MountEntry struct {
	Source     string
	Mountpoint string
	FSType     string
}

// StatfsResult holds filesystem space statistics for a mount point.
type StatfsResult struct {
	TotalSize uint64
	FreeSpace uint64
}
