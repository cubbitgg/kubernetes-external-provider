package providers

import (
	"fmt"
	"syscall"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/models"
)

// StatfsProvider abstracts syscall.Statfs for testability.
type StatfsProvider interface {
	Statfs(path string) (*models.StatfsResult, error)
}

type realStatfsProvider struct{}

// NewStatfsProvider returns a StatfsProvider backed by syscall.Statfs.
func NewStatfsProvider() StatfsProvider {
	return &realStatfsProvider{}
}

func (p *realStatfsProvider) Statfs(path string) (*models.StatfsResult, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("statfs %q: %w", path, err)
	}
	return &models.StatfsResult{
		TotalSize: stat.Blocks * uint64(stat.Bsize),
		FreeSpace: stat.Bfree * uint64(stat.Bsize),
	}, nil
}
