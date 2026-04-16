package common

const (
	// ProvisionerName is the name used in StorageClass.provisioner and
	// the pv.kubernetes.io/provisioned-by annotation.
	ProvisionerName = "agent.cubbit.io/local-disk"

	// LabelUUIDPrefix is prepended to a disk UUID to form a node label key.
	// Full key: "agent.cubbit.io/has-uuid-<UUID>" (name part = 45 chars, under the 63-char limit).
	LabelUUIDPrefix = "agent.cubbit.io/has-uuid-"

	// AnnotationDiskMap is a node annotation containing a JSON map of
	// UUID -> DiskEntry (path + size in bytes). Written by the node-scanner.
	AnnotationDiskMap = "agent.cubbit.io/disk-uuid-map"

	// PVCAnnotationUUID is an annotation the user places on a PVC to declare
	// which disk UUID they want provisioned.
	PVCAnnotationUUID = "agent.cubbit.io/disk-uuid"

	// AnnotationProvisionedBy is set on PVs created by this provisioner so
	// Delete() can verify ownership.
	AnnotationProvisionedBy = "agent.cubbit.io/provisioned-by"

	// DefaultMountBase is the base directory under which disks are mounted by
	// the node-scanner (e.g. /mnt/cubbit/<uuid>).
	DefaultMountBase = "/mnt/cubbit"

	// DefaultFSType is the filesystem type used when scanning/mounting/formatting.
	DefaultFSType = "ext4"

	// DefaultMinSize is the minimum disk size (bytes) to consider (50 MiB).
	DefaultMinSize = uint64(52428800)
)
