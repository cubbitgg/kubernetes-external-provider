package fsutils_test

import (
	"encoding/json"
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/fsutils"
)

// --- Filter combinators ---

func device(typ, fstype string) *fsutils.BlockDevice {
	return &fsutils.BlockDevice{Type: typ, FSType: fstype, Name: "/dev/test"}
}

func TestUnit_TypeFilter_Match(t *testing.T) {
	f := fsutils.TypeFilter("disk", "part")
	_, skip, _ := f(device("disk", ""))
	if skip {
		t.Error("expected disk type to be included")
	}
}

func TestUnit_TypeFilter_NoMatch(t *testing.T) {
	f := fsutils.TypeFilter("disk")
	_, skip, _ := f(device("loop", ""))
	if !skip {
		t.Error("expected loop type to be skipped")
	}
}

func TestUnit_FSTypeFilter_Match(t *testing.T) {
	f := fsutils.FSTypeFilter("ext4", "vfat")
	_, skip, _ := f(device("disk", "ext4"))
	if skip {
		t.Error("expected ext4 to be included")
	}
}

func TestUnit_FSTypeFilter_NoMatch(t *testing.T) {
	f := fsutils.FSTypeFilter("ext4")
	_, skip, _ := f(device("disk", "xfs"))
	if !skip {
		t.Error("expected xfs to be skipped when not in filter list")
	}
}

func TestUnit_FSTypeFilter_EmptyFSType(t *testing.T) {
	// Devices with empty FSType (unformatted) must NOT be filtered out
	f := fsutils.FSTypeFilter("ext4")
	_, skip, _ := f(device("disk", ""))
	if skip {
		t.Error("expected unformatted device (empty fstype) to pass FSTypeFilter")
	}
}

func TestUnit_NameFilter_Match(t *testing.T) {
	f := fsutils.NameFilter("/dev/sda")
	bd := &fsutils.BlockDevice{Name: "/dev/sda"}
	_, skip, _ := f(bd)
	if skip {
		t.Error("expected matching name to be included")
	}
}

func TestUnit_NameFilter_NoMatch(t *testing.T) {
	f := fsutils.NameFilter("/dev/sda")
	bd := &fsutils.BlockDevice{Name: "/dev/sdb"}
	_, skip, _ := f(bd)
	if !skip {
		t.Error("expected non-matching name to be skipped")
	}
}

func TestUnit_AndFilter_AllPass(t *testing.T) {
	f := fsutils.And(
		fsutils.TypeFilter("disk"),
		fsutils.FSTypeFilter("ext4"),
	)
	_, skip, _ := f(&fsutils.BlockDevice{Type: "disk", FSType: "ext4"})
	if skip {
		t.Error("expected AND of two passing filters to include device")
	}
}

func TestUnit_AndFilter_OneSkips(t *testing.T) {
	f := fsutils.And(
		fsutils.TypeFilter("disk"),
		fsutils.FSTypeFilter("ext4"),
	)
	_, skip, _ := f(&fsutils.BlockDevice{Type: "loop", FSType: "ext4"})
	if !skip {
		t.Error("expected AND filter to skip when one sub-filter skips")
	}
}

func TestUnit_OrFilter_OnePass(t *testing.T) {
	f := fsutils.Or(
		fsutils.TypeFilter("disk"),
		fsutils.TypeFilter("loop"),
	)
	_, skip, _ := f(&fsutils.BlockDevice{Type: "loop"})
	if skip {
		t.Error("expected OR filter to include device when one sub-filter passes")
	}
}

func TestUnit_OrFilter_AllSkip(t *testing.T) {
	f := fsutils.Or(
		fsutils.TypeFilter("disk"),
		fsutils.TypeFilter("part"),
	)
	_, skip, _ := f(&fsutils.BlockDevice{Type: "loop"})
	if !skip {
		t.Error("expected OR filter to skip device when all sub-filters skip")
	}
}

func TestUnit_NotFilter_Inverts(t *testing.T) {
	f := fsutils.Not(fsutils.TypeFilter("disk"))

	_, skipDisk, _ := f(&fsutils.BlockDevice{Type: "disk"})
	if !skipDisk {
		t.Error("expected NOT(TypeFilter(disk)) to skip a disk")
	}

	_, skipLoop, _ := f(&fsutils.BlockDevice{Type: "loop"})
	if skipLoop {
		t.Error("expected NOT(TypeFilter(disk)) to include a loop device")
	}
}

// --- Utility functions ---

func TestUnit_FlattenDevices(t *testing.T) {
	parent := fsutils.BlockDevice{
		Name: "/dev/sda",
		Children: []fsutils.BlockDevice{
			{Name: "/dev/sda1"},
			{Name: "/dev/sda2"},
		},
	}

	flat := fsutils.FlattenDevices([]fsutils.BlockDevice{parent})
	if len(flat) != 3 {
		t.Errorf("expected 3 devices (parent + 2 children), got %d", len(flat))
	}
}

func TestUnit_FilterDevices(t *testing.T) {
	devices := []fsutils.BlockDevice{
		{Name: "/dev/sda", Type: "disk"},
		{Name: "/dev/sda1", Type: "part"},
		{Name: "/dev/loop0", Type: "loop"},
	}

	result := fsutils.FilterDevices(devices, func(bd fsutils.BlockDevice) bool {
		return bd.Type == "disk"
	})

	if len(result) != 1 || result[0].Name != "/dev/sda" {
		t.Errorf("expected 1 disk, got %+v", result)
	}
}

// --- Custom JSON unmarshalers ---

func TestUnit_CustomInt64_UnmarshalInteger(t *testing.T) {
	var v fsutils.CustomInt64
	if err := json.Unmarshal([]byte(`1073741824`), &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int64(v) != 1073741824 {
		t.Errorf("expected 1073741824, got %d", v)
	}
}

func TestUnit_CustomInt64_UnmarshalString(t *testing.T) {
	var v fsutils.CustomInt64
	if err := json.Unmarshal([]byte(`"512"`), &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int64(v) != 512 {
		t.Errorf("expected 512, got %d", v)
	}
}

func TestUnit_CustomInt64_UnmarshalEmptyString(t *testing.T) {
	var v fsutils.CustomInt64
	if err := json.Unmarshal([]byte(`""`), &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int64(v) != 0 {
		t.Errorf("expected 0 for empty string, got %d", v)
	}
}

func TestUnit_CustomBool_UnmarshalBool(t *testing.T) {
	var v fsutils.CustomBool
	if err := json.Unmarshal([]byte(`true`), &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bool(v) {
		t.Error("expected true")
	}
}

func TestUnit_CustomBool_UnmarshalStringTrue(t *testing.T) {
	for _, raw := range []string{`"true"`, `"1"`, `"yes"`} {
		var v fsutils.CustomBool
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatalf("unexpected error for %s: %v", raw, err)
		}
		if !bool(v) {
			t.Errorf("expected true for %s", raw)
		}
	}
}

func TestUnit_CustomBool_UnmarshalStringFalse(t *testing.T) {
	for _, raw := range []string{`"false"`, `"0"`, `"no"`, `""`} {
		var v fsutils.CustomBool
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatalf("unexpected error for %s: %v", raw, err)
		}
		if bool(v) {
			t.Errorf("expected false for %s", raw)
		}
	}
}

// --- FindRootDiskDescendants ---

func TestUnit_FindRootDiskDescendants_CollectsTree(t *testing.T) {
	devices := []fsutils.BlockDevice{
		{
			Name: "/dev/sda", Type: "disk",
			Children: []fsutils.BlockDevice{
				{Name: "/dev/sda1", Type: "part"},
				{Name: "/dev/sda2", Type: "part"}, // root source
			},
		},
		{
			Name:     "/dev/sdb",
			Type:     "disk",
			Children: []fsutils.BlockDevice{{Name: "/dev/sdb1", Type: "part"}},
		},
	}

	excluded := fsutils.FindRootDiskDescendants(devices, "/dev/sda2")
	for _, want := range []string{"/dev/sda", "/dev/sda1", "/dev/sda2"} {
		if _, ok := excluded[want]; !ok {
			t.Errorf("expected %s in excluded set, not found", want)
		}
	}
	// Unrelated disk must not be in excluded set.
	for _, notWant := range []string{"/dev/sdb", "/dev/sdb1"} {
		if _, ok := excluded[notWant]; ok {
			t.Errorf("expected %s NOT in excluded set, but it was", notWant)
		}
	}
}

func TestUnit_FindRootDiskDescendants_RootNotFound_ReturnsEmpty(t *testing.T) {
	devices := []fsutils.BlockDevice{
		{Name: "/dev/sda", Type: "disk"},
	}
	excluded := fsutils.FindRootDiskDescendants(devices, "/dev/nvme0n1p2")
	if len(excluded) != 0 {
		t.Errorf("expected empty excluded set when root not found, got %v", excluded)
	}
}

// --- ValidateLabel ---

func TestUnit_ValidateLabel_Valid(t *testing.T) {
	valid := []string{"CUBBIT", "DATA1", "A", "0123456789"}
	for _, l := range valid {
		if err := fsutils.ValidateLabel(l); err != nil {
			t.Errorf("expected label %q to be valid, got: %v", l, err)
		}
	}
}

func TestUnit_ValidateLabel_Empty(t *testing.T) {
	if err := fsutils.ValidateLabel(""); err == nil {
		t.Error("expected error for empty label")
	}
}

func TestUnit_ValidateLabel_TooLong(t *testing.T) {
	if err := fsutils.ValidateLabel("TOOLONGLABEL"); err == nil {
		t.Error("expected error for label > 10 chars")
	}
}

func TestUnit_ValidateLabel_Lowercase(t *testing.T) {
	if err := fsutils.ValidateLabel("cubbit"); err == nil {
		t.Error("expected error for lowercase label")
	}
}

func TestUnit_ValidateLabel_Punctuation(t *testing.T) {
	if err := fsutils.ValidateLabel("CUB-BIT"); err == nil {
		t.Error("expected error for label with hyphen")
	}
}
