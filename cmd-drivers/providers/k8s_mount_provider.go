package providers

import (
	"fmt"

	"k8s.io/mount-utils"
)

// K8sMountProvider abstracts k8s.io/mount-utils for testability.
type K8sMountProvider interface {
	Mount(source, target, fstype string, options []string) error
	Unmount(target string) error
	IsLikelyNotMountPoint(file string) (bool, error)
}

type realK8sMountProvider struct {
	mounter mount.Interface
}

// NewK8sMountProvider returns a K8sMountProvider backed by k8s.io/mount-utils.
func NewK8sMountProvider() K8sMountProvider {
	return &realK8sMountProvider{mounter: mount.New("")}
}

func (p *realK8sMountProvider) Mount(source, target, fstype string, options []string) error {
	if err := p.mounter.Mount(source, target, fstype, options); err != nil {
		return fmt.Errorf("mount %q → %q: %w", source, target, err)
	}
	return nil
}

func (p *realK8sMountProvider) Unmount(target string) error {
	if err := p.mounter.Unmount(target); err != nil {
		return fmt.Errorf("unmount %q: %w", target, err)
	}
	return nil
}

func (p *realK8sMountProvider) IsLikelyNotMountPoint(file string) (bool, error) {
	return p.mounter.IsLikelyNotMountPoint(file)
}
