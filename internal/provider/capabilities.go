package provider

import (
	"fmt"
	"slices"
)

const (
	// CapabilityLocalAgent means the provider has a shipped local runner.
	CapabilityLocalAgent = "local-agent"
	// CapabilityNativeFanout means the provider has verified internal fan-out.
	CapabilityNativeFanout = "native-fanout"
)

var calibratedCapabilityVocabulary = []string{
	"runtime.local-session",
	"runtime.noninteractive",
	"runtime.structured-output",
	"runtime.usage-metered",
	"runtime.resume",
	"runtime.native-fanout",
	"context.16k",
	"context.128k",
	"input.image",
	"tools.repo-read",
	"tools.repo-write",
	"tools.shell",
	"tools.browser-silent",
	"quality.exact-citation",
	"quality.schema-conformance",
	"quality.graph-reasoning",
	"quality.causal-narrative",
	"quality.quantitative-systems",
	"quality.code-build-test",
	"quality.visual-critique",
	"quality.pixel-composition",
}

// KnownCapability reports whether a task requirement is part of Ralph's
// closed capability vocabulary.
func KnownCapability(name string) bool {
	switch name {
	case CapabilityLocalAgent, CapabilityNativeFanout:
		return true
	default:
		return slices.Contains(calibratedCapabilityVocabulary, name)
	}
}

// CalibrationRequiredCapability reports capabilities that may only be granted
// by measured fixture evidence, never by a built-in flag or CLI help text.
func CalibrationRequiredCapability(name string) bool {
	return slices.Contains(calibratedCapabilityVocabulary, name)
}

// SupportsRequirements reports whether a binding satisfies every requirement.
// Unknown capabilities fail closed.
func (b Binding) SupportsRequirements(requirements []string) bool {
	for _, requirement := range requirements {
		if slices.Contains(b.CalibratedCapabilities, requirement) {
			continue
		}
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
