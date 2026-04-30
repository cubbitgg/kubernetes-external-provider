package commonlib

// DiskEntry is the value type stored in the node annotation disk map.
// Written by the node-scanner, read by the provisioner.
type DiskEntry struct {
	Path string `json:"path"`
	Size uint64 `json:"size"`
}
