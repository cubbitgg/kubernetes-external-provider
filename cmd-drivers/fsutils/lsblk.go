package fsutils

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/logger"
	"github.com/rs/zerolog"
)

// BlockDevice represents a block device with all its properties
type BlockDevice struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Size       CustomInt64   `json:"size"`
	Rota       CustomBool    `json:"rota"`
	Serial     string        `json:"serial"`
	WWN        string        `json:"wwn"`
	Vendor     string        `json:"vendor"`
	Model      string        `json:"model"`
	Rev        string        `json:"rev"`
	Mountpoint string        `json:"mountpoint"`
	FSType     string        `json:"fstype"`
	UUID       string        `json:"uuid"`
	PartUUID   string        `json:"partuuid"`
	Label      string        `json:"label"`
	PKName     string        `json:"pkname"`
	Children   []BlockDevice `json:"children,omitempty"`
}

// CustomInt64 handles both string and integer JSON values for size fields
type CustomInt64 int64

// UnmarshalJSON implements custom unmarshaling for int64 that can be string or number
func (ci *CustomInt64) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as int64 first
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*ci = CustomInt64(i)
		return nil
	}

	// Try to unmarshal as string
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Empty string means 0
	if s == "" {
		*ci = 0
		return nil
	}

	// Parse string to int64
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse %q as int64: %w", s, err)
	}

	*ci = CustomInt64(i)
	return nil
}

// MarshalJSON implements custom marshaling for CustomInt64
func (ci CustomInt64) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(ci))
}

// CustomBool handles both string and boolean JSON values
type CustomBool bool

// UnmarshalJSON implements custom unmarshaling for bool that can be string or boolean
func (cb *CustomBool) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as bool first
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*cb = CustomBool(b)
		return nil
	}

	// Try to unmarshal as string
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Parse string representations
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		*cb = true
	case "false", "0", "no", "":
		*cb = false
	default:
		return fmt.Errorf("invalid boolean value: %q", s)
	}

	return nil
}

// MarshalJSON implements custom marshaling for CustomBool
func (cb CustomBool) MarshalJSON() ([]byte, error) {
	return json.Marshal(bool(cb))
}

// lsblkOutput represents the JSON output structure from lsblk
type lsblkOutput struct {
	BlockDevices []BlockDevice `json:"blockdevices"`
}

// LSBLK is the interface for interacting with lsblk utility
type LSBLK interface {
	GetBlockDevices(ctx context.Context, filter FilterFunc) ([]BlockDevice, error)
	GetBlockDevice(ctx context.Context, device string) (*BlockDevice, error)
	GetUUID(ctx context.Context, device string) (string, error)
}

// FilterFunc is a type defining a callback function for GetBlockDevices,
// used to filter out block devices we're not interested in, and/or stop further processing.
// It takes a pointer to the BlockDevice struct and returns:
// - name: the name of the filter (for logging/debugging)
// - skip: if true, skip this device (exclude from results)
// - stop: if true, stop processing remaining devices
type FilterFunc func(*BlockDevice) (name string, skip bool, stop bool)

// lsblkImpl is the default implementation of LSBLK interface
type lsblkImpl struct{}

// NewLSBLK creates a new LSBLK instance
func NewLSBLK() LSBLK {
	return &lsblkImpl{}
}

// GetBlockDevices runs lsblk command and returns block devices
// Applies an optional filter function to select which devices to return (use nil for no filter)
func (l *lsblkImpl) GetBlockDevices(ctx context.Context, filter FilterFunc) ([]BlockDevice, error) {
	log := logger.FromContext(ctx)

	log.Debug().Msg("Getting block devices")

	// Build command - always get all devices, filter afterwards
	args := []string{
		"--paths",
		"--json",
		"--bytes",
		"--output", "NAME,TYPE,SIZE,ROTA,SERIAL,WWN,VENDOR,MODEL,REV,MOUNTPOINT,FSTYPE,UUID,PARTUUID,LABEL,PKNAME",
	}

	// Execute command
	cmd := exec.CommandContext(ctx, "lsblk", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Error().
				Err(err).
				Str("stderr", string(exitErr.Stderr)).
				Msg("lsblk command failed")
			return nil, fmt.Errorf("lsblk command failed: %w, stderr: %s", err, string(exitErr.Stderr))
		}
		log.Error().
			Err(err).
			Msg("Failed to execute lsblk")
		return nil, fmt.Errorf("failed to execute lsblk: %w", err)
	}

	log.Debug().
		Int("output_size", len(output)).
		Msg("lsblk command executed successfully")

	// Parse JSON output
	var result lsblkOutput
	if err := json.Unmarshal(output, &result); err != nil {
		log.Error().
			Err(err).
			Str("output", string(output)).
			Msg("Failed to unmarshal lsblk output")
		return nil, fmt.Errorf("failed to unmarshal lsblk output: %w", err)
	}

	// Apply filter if provided
	if filter == nil {
		log.Debug().
			Int("device_count", len(result.BlockDevices)).
			Msg("Successfully parsed block devices (no filter)")
		return result.BlockDevices, nil
	}

	// Filter devices (including children recursively)
	var filtered []BlockDevice
	for _, device := range result.BlockDevices {
		filtered = append(filtered, filterDeviceRecursive(&device, filter, log)...)
	}

	log.Debug().
		Int("device_count", len(filtered)).
		Int("total_devices", len(result.BlockDevices)).
		Msg("Successfully parsed and filtered block devices")

	return filtered, nil
}

// filterDeviceRecursive applies filter to a device and its children recursively
func filterDeviceRecursive(device *BlockDevice, filter FilterFunc, log zerolog.Logger) []BlockDevice {
	var result []BlockDevice

	name, skip, stop := filter(device)

	if skip {
		log.Debug().
			Str("device", device.Name).
			Str("type", device.Type).
			Str("fstype", device.FSType).
			Str("mountpoint", device.Mountpoint).
			Str("filter", name).
			Bool("skip", skip).
			Bool("stop", stop).
			Msg("Device filtered out")
		if stop {
			return result
		}
		// Don't include this device, but continue with children
	} else {
		// Include this device
		result = append(result, *device)

		if stop {
			log.Debug().
				Str("device", device.Name).
				Str("type", device.Type).
				Str("filter", name).
				Bool("skip", skip).
				Bool("stop", stop).
				Msg("Filter requested stop after including device")
			return result
		}
	}

	// Process children recursively
	for _, child := range device.Children {
		childCopy := child // Create a copy to avoid pointer issues
		result = append(result, filterDeviceRecursive(&childCopy, filter, log)...)
	}

	return result
}

// GetBlockDevice returns information about a single block device
// Returns an error if the device is not found or if device parameter is empty
func (l *lsblkImpl) GetBlockDevice(ctx context.Context, device string) (*BlockDevice, error) {
	log := logger.FromContext(ctx)

	if device == "" {
		log.Error().Msg("Device parameter cannot be empty")
		return nil, fmt.Errorf("device parameter cannot be empty")
	}

	log.Debug().
		Str("device", device).
		Msg("Getting single block device")

	// Create a filter for the specific device
	deviceFilter := func(bd *BlockDevice) (name string, skip bool, stop bool) {
		if bd.Name == device {
			return "DeviceNameMatch", false, true // Found it, stop processing
		}
		return "DeviceNameMatch", true, false // Skip this one, keep looking
	}

	devices, err := l.GetBlockDevices(ctx, deviceFilter)
	if err != nil {
		return nil, err
	}

	if len(devices) == 0 {
		log.Warn().
			Str("device", device).
			Msg("Device not found")
		return nil, fmt.Errorf("device %q not found", device)
	}

	log.Debug().
		Str("device", device).
		Str("name", devices[0].Name).
		Str("type", devices[0].Type).
		Msg("Found block device")

	return &devices[0], nil
}

// FlattenDevices returns a flat list of all devices including children
func FlattenDevices(devices []BlockDevice) []BlockDevice {
	var result []BlockDevice

	for _, device := range devices {
		result = append(result, device)
		if len(device.Children) > 0 {
			result = append(result, FlattenDevices(device.Children)...)
		}
	}

	return result
}

// FilterDevices filters devices by a predicate function
func FilterDevices(devices []BlockDevice, predicate func(BlockDevice) bool) []BlockDevice {
	var result []BlockDevice

	for _, device := range devices {
		if predicate(device) {
			result = append(result, device)
		}
	}

	return result
}

// GetUUID returns the UUID for a given device path
// Returns UUID if available, otherwise returns PARTUUID as fallback
// Returns empty string if neither is available
func (l *lsblkImpl) GetUUID(ctx context.Context, device string) (string, error) {
	log := logger.FromContext(ctx)

	if device == "" {
		log.Error().Msg("Device parameter cannot be empty for GetUUID")
		return "", fmt.Errorf("device parameter cannot be empty")
	}

	// Skip non-device sources
	if !strings.HasPrefix(device, "/dev/") {
		log.Debug().
			Str("device", device).
			Msg("Skipping non-device source")
		return "", nil
	}

	log.Debug().
		Str("device", device).
		Msg("Getting UUID for device")

	// Try to get device info directly
	blockDevice, err := l.GetBlockDevice(ctx, device)
	if err == nil {
		// Return UUID if available
		if blockDevice.UUID != "" {
			log.Debug().
				Str("device", device).
				Str("uuid", blockDevice.UUID).
				Msg("Found UUID for device")
			return blockDevice.UUID, nil
		}
		// Fallback to PARTUUID
		if blockDevice.PartUUID != "" {
			log.Debug().
				Str("device", device).
				Str("partuuid", blockDevice.PartUUID).
				Msg("Using PARTUUID as fallback")
			return blockDevice.PartUUID, nil
		}
		log.Debug().
			Str("device", device).
			Msg("No UUID or PARTUUID found for device")
		return "", nil
	}

	log.Debug().
		Str("device", device).
		Err(err).
		Msg("Direct lookup failed, searching all devices")

	// If direct lookup fails, try to find it in all devices
	allDevices, err := l.GetBlockDevices(ctx, nil)
	if err != nil {
		log.Error().
			Err(err).
			Str("device", device).
			Msg("Failed to get all block devices")
		return "", fmt.Errorf("failed to get block devices: %w", err)
	}

	// Flatten and search for the device
	flatDevices := FlattenDevices(allDevices)
	for _, dev := range flatDevices {
		if dev.Name == device {
			if dev.UUID != "" {
				log.Debug().
					Str("device", device).
					Str("uuid", dev.UUID).
					Msg("Found UUID in flattened devices")
				return dev.UUID, nil
			}
			// Fallback to PARTUUID if UUID is not available
			if dev.PartUUID != "" {
				log.Debug().
					Str("device", device).
					Str("partuuid", dev.PartUUID).
					Msg("Found PARTUUID in flattened devices")
				return dev.PartUUID, nil
			}
			log.Debug().
				Str("device", device).
				Msg("Found device but no UUID or PARTUUID")
			return "", nil
		}
	}

	log.Warn().
		Str("device", device).
		Msg("Device not found in any search")
	return "", fmt.Errorf("device %q not found", device)
}

// FindRootDiskDescendants returns the set of device paths (including the disk
// itself) that belong to the same disk as rootSource. rootSource is the device
// path backing "/" (e.g. "/dev/nvme0n1p2"). The returned set contains every
// name in the disk's subtree so the caller can check membership in O(1).
// Returns an empty set (not an error) when rootSource is not found in devices —
// the caller should treat that as "root disk unknown, fail-open".
func FindRootDiskDescendants(devices []BlockDevice, rootSource string) map[string]struct{} {
	flat := FlattenDevices(devices)
	// Build a fast name→exists lookup.
	all := make(map[string]struct{}, len(flat))
	for _, d := range flat {
		all[d.Name] = struct{}{}
	}

	// Find the top-level disk that contains rootSource in its subtree.
	for i := range devices {
		subtree := FlattenDevices([]BlockDevice{devices[i]})
		for _, d := range subtree {
			if d.Name == rootSource {
				// Collect every name in this disk's full subtree.
				excluded := make(map[string]struct{}, len(subtree))
				for _, s := range subtree {
					excluded[s.Name] = struct{}{}
				}
				return excluded
			}
		}
	}
	return map[string]struct{}{}
}

// And combines multiple FilterFunc with AND logic.
// All filters must pass (not skip) for the device to be included.
// If any filter returns stop=true, processing stops immediately.
func And(filters ...FilterFunc) FilterFunc {
	return func(device *BlockDevice) (name string, skip bool, stop bool) {
		filterNames := make([]string, 0, len(filters))
		for _, filter := range filters {
			filterName, filterSkip, filterStop := filter(device)
			filterNames = append(filterNames, filterName)
			if filterSkip || filterStop {
				return strings.Join(filterNames, " AND "), filterSkip, filterStop
			}
		}
		return strings.Join(filterNames, " AND "), false, false
	}
}

// Or combines multiple FilterFunc with OR logic.
// If any filter passes (does not skip), the device is included.
// Processing stops if any filter returns stop=true.
func Or(filters ...FilterFunc) FilterFunc {
	return func(device *BlockDevice) (name string, skip bool, stop bool) {
		filterNames := make([]string, 0, len(filters))
		allSkipped := true
		for _, filter := range filters {
			filterName, filterSkip, filterStop := filter(device)
			filterNames = append(filterNames, filterName)
			if filterStop {
				return strings.Join(filterNames, " OR "), filterSkip, filterStop
			}
			if !filterSkip {
				allSkipped = false
			}
		}
		return strings.Join(filterNames, " OR "), allSkipped, false
	}
}

// Not inverts a FilterFunc.
// If the filter would skip, don't skip. If it wouldn't skip, skip.
func Not(filter FilterFunc) FilterFunc {
	return func(device *BlockDevice) (name string, skip bool, stop bool) {
		filterName, filterSkip, filterStop := filter(device)
		return "NOT " + filterName, !filterSkip, filterStop
	}
}

// TypeFilter returns a FilterFunc that only includes devices of specified types
func TypeFilter(types ...string) FilterFunc {
	typeMap := make(map[string]bool)
	for _, t := range types {
		typeMap[t] = true
	}

	return func(device *BlockDevice) (name string, skip bool, stop bool) {
		if typeMap[device.Type] {
			return "TypeFilter", false, false
		}
		return "TypeFilter", true, false
	}
}

// NameFilter returns a FilterFunc that only includes devices matching the specified name
func NameFilter(name string) FilterFunc {
	return func(device *BlockDevice) (filterName string, skip bool, stop bool) {
		if device.Name == name {
			return "NameFilter", false, false
		}
		return "NameFilter", true, false
	}
}

// FSTypeFilter returns a FilterFunc that only includes devices with specified filesystem types
// Devices with empty FSType (unformatted) are NOT filtered out
func FSTypeFilter(fsTypes ...string) FilterFunc {
	fsTypeMap := make(map[string]bool)
	for _, fs := range fsTypes {
		fsTypeMap[fs] = true
	}

	return func(device *BlockDevice) (name string, skip bool, stop bool) {
		// Don't filter out unformatted devices (empty FSType)
		if device.FSType == "" {
			return "FSTypeFilter", false, false
		}

		// Check if FSType matches any of the specified types
		if fsTypeMap[device.FSType] {
			return "FSTypeFilter", false, false
		}
		return "FSTypeFilter", true, false
	}
}
