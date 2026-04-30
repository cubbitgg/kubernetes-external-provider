package mocks

import (
	"context"

	"github.com/cubbitgg/kubernetes-external-provider/cmd-drivers/providers"
)

// MockFormatProvider is a test double for providers.FormatProvider.
// Set FormatFunc before use — leaving it nil will panic, forcing explicit test setup.
type MockFormatProvider struct {
	FormatFunc func(ctx context.Context, device string, opts providers.FormatOptions) error
}

func (m *MockFormatProvider) Format(ctx context.Context, device string, opts providers.FormatOptions) error {
	return m.FormatFunc(ctx, device, opts)
}
