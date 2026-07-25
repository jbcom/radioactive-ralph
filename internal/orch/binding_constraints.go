package orch

import (
	"context"
	"errors"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// BindingConstraints are the fail-closed provider requirements of one task.
type BindingConstraints struct {
	AllowedProviders []string
	DeniedProviders  []string
	Requirements     []string
}

// ConstrainedBindingResolver selects a provider after applying task metadata.
type ConstrainedBindingResolver func(
	ctx context.Context,
	projectID string,
	parallelGroup bool,
	purpose BindingResolutionPurpose,
	constraints BindingConstraints,
) (provider.Binding, error)

// ErrNoCapableProvider is returned when no configured provider can satisfy a
// task's allowlist, separation, and capability constraints.
var ErrNoCapableProvider = errors.New("orch: no capable provider")

func noCapableProvider(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNoCapableProvider, fmt.Sprintf(format, args...))
}

func unconstrainedAdapter(resolve BindingResolver) ConstrainedBindingResolver {
	return func(
		ctx context.Context,
		projectID string,
		parallel bool,
		purpose BindingResolutionPurpose,
		constraints BindingConstraints,
	) (provider.Binding, error) {
		if len(constraints.AllowedProviders)+len(constraints.DeniedProviders)+len(constraints.Requirements) > 0 {
			return provider.Binding{}, noCapableProvider("resolver does not implement task constraints")
		}
		return resolve(ctx, projectID, parallel, purpose)
	}
}
