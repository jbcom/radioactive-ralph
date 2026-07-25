package main

import (
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// providerSelectionFromValues resolves one config layer's provider selection.
// The canonical shape is providers=[...]. The legacy singular provider key is
// accepted as a one-element alias, but both keys in one layer are ambiguous and
// therefore rejected.
func providerSelectionFromValues(values map[string]any) (names []string, found bool, err error) {
	poolValue, hasPool := values[providersConfigKey]
	singleValue, hasSingle := values[providerConfigKey]
	if hasPool && hasSingle {
		return nil, false, fmt.Errorf("%s and legacy %s cannot both be set", providersConfigKey, providerConfigKey)
	}

	switch {
	case hasPool:
		names, err = stringSliceValue(poolValue)
	case hasSingle:
		name, ok := stringValue(singleValue)
		if !ok {
			err = fmt.Errorf("must be a non-empty provider name")
		} else {
			names = []string{name}
		}
	default:
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := validateProviderNames(names); err != nil {
		return nil, false, err
	}
	return names, true, nil
}

// normalizeIncomingProviderSelection converts a legacy singular provider
// selection into the canonical providers array. The returned map is always a
// copy, so validation never mutates the caller's decoded config.
func normalizeIncomingProviderSelection(values map[string]any) (normalized map[string]any, selectionFound bool, err error) {
	normalized = make(map[string]any, len(values))
	for key, value := range values {
		normalized[key] = value
	}

	names, found, err := providerSelectionFromValues(values)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return normalized, false, nil
	}

	delete(normalized, providerConfigKey)
	normalized[providersConfigKey] = names
	return normalized, true, nil
}

// validateProviderNames validates the whole selection before any member can be
// assigned. This prevents a typo in one pool entry from allowing earlier
// members to dispatch work before the bad entry is eventually reached.
func validateProviderNames(names []string) error {
	for _, name := range names {
		if _, err := provider.ResolveBinding(
			provider.File{DefaultProvider: name},
			provider.Local{},
			provider.VariantFile{},
		); err != nil {
			return fmt.Errorf("provider %q: %w", name, err)
		}
	}
	return nil
}
