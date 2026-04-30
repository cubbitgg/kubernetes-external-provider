package fsutils

import (
	"fmt"
	"strings"
)

// FSType represents a supported filesystem type.
type FSType string

const (
	FSTypeExt4 FSType = "ext4"
	FSTypeXFS  FSType = "xfs"
	FSTypeVFAT FSType = "vfat"
	FSTypeNTFS FSType = "ntfs"
)

// ValidFSTypes is the list of all supported filesystem types.
var ValidFSTypes = []FSType{
	FSTypeExt4,
	FSTypeXFS,
	FSTypeVFAT,
	FSTypeNTFS,
}

// IsValidFSType reports whether s is a supported filesystem type.
func IsValidFSType(s string) bool {
	for _, t := range ValidFSTypes {
		if FSType(s) == t {
			return true
		}
	}
	return false
}

// ParseFSTypes parses a comma-separated string of filesystem type names into a
// slice of FSType values. Returns an error if any entry is not a supported type.
func ParseFSTypes(csv string) ([]FSType, error) {
	parts := strings.Split(csv, ",")
	result := make([]FSType, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if !IsValidFSType(trimmed) {
			return nil, fmt.Errorf("unsupported filesystem type %q: must be one of %s", trimmed, validFSTypeNames())
		}
		result = append(result, FSType(trimmed))
	}
	return result, nil
}

// FSTypesToStrings converts a slice of FSType to a slice of strings,
// for use with services and providers that accept []string.
func FSTypesToStrings(types []FSType) []string {
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
}

// ValidFSTypeList returns a human-readable comma-separated list of valid FSType values,
// suitable for use in flag help text.
func ValidFSTypeList() string {
	return validFSTypeNames()
}

func validFSTypeNames() string {
	names := make([]string, len(ValidFSTypes))
	for i, t := range ValidFSTypes {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}

// ValidateLabel enforces the portable subset that works on every fs type this
// tool supports (worst-case vfat): max 10 chars, A–Z and 0–9 only.
func ValidateLabel(label string) error {
	if len(label) == 0 || len(label) > 10 {
		return fmt.Errorf("label %q: length must be 1–10 chars", label)
	}
	for _, r := range label {
		if !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return fmt.Errorf("label %q: only uppercase A-Z and digits 0-9 are allowed", label)
		}
	}
	return nil
}
