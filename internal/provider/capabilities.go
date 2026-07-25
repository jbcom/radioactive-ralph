package provider

import "fmt"

const (
	// CapabilityLocalAgent means the provider has a shipped local runner.
	CapabilityLocalAgent = "local-agent"
	// CapabilityNativeFanout means the provider has verified internal fan-out.
	CapabilityNativeFanout = "native-fanout"
)

// KnownCapability reports whether a task requirement is part of Ralph's
// closed capability vocabulary.
func KnownCapability(name string) bool {
	switch name {
	case CapabilityLocalAgent, CapabilityNativeFanout:
		return true
	default:
		return false
	}
}

// SupportsRequirements reports whether a binding satisfies every requirement.
// Unknown capabilities fail closed.
func (b Binding) SupportsRequirements(requirements []string) bool {
	for _, requirement := range requirements {
		switch requirement {
		case CapabilityLocalAgent:
			if _, err := NewRunner(b); err != nil {
				return false
			}
		case CapabilityNativeFanout:
			if !b.Config.NativeFanout {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ResolveShippedBinding resolves a built-in provider by its stable name.
func ResolveShippedBinding(name string) (Binding, error) {
	if name == "" {
		return Binding{}, fmt.Errorf("provider name required")
	}
	return ResolveBinding(File{DefaultProvider: name}, Local{}, VariantFile{})
}
