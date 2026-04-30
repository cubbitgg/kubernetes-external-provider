package providers_test

import (
	"testing"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
)

func TestIntegration_StatfsProvider_RealPath(t *testing.T) {
	p := providers.NewStatfsProvider()

	result, err := p.Statfs("/")
	if err != nil {
		t.Fatalf("unexpected error calling Statfs(/): %v", err)
	}
	if result.TotalSize == 0 {
		t.Error("expected non-zero TotalSize for /")
	}
	if result.FreeSpace == 0 {
		t.Error("expected non-zero FreeSpace for /")
	}
	if result.FreeSpace > result.TotalSize {
		t.Errorf("FreeSpace (%d) > TotalSize (%d), which is impossible", result.FreeSpace, result.TotalSize)
	}
}

func TestIntegration_StatfsProvider_InvalidPath(t *testing.T) {
	p := providers.NewStatfsProvider()

	_, err := p.Statfs("/this/path/does/not/exist/at/all")
	if err == nil {
		t.Fatal("expected error for nonexistent path, got nil")
	}
}
