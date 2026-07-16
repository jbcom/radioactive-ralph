package vconfig

import "strings"

// MissingField describes one required config key absent (or present but
// empty) from a merged ProjectConfig.
type MissingField struct {
	Key    string
	Reason string
}

// Validate checks cfg against required, reporting one MissingField per key
// that is absent or holds an empty value ("", nil, or an empty
// string-keyed/slice value). The merged layer (DB < configFile <
// userConfigFile < projectConfigFile, per EffectiveProject) is what callers
// should pass in — Validate itself does no layering.
func Validate(cfg ProjectConfig, required []string) []MissingField {
	var missing []MissingField
	for _, key := range required {
		v, ok := cfg.Values[key]
		if !ok || isEmptyValue(v) {
			missing = append(missing, MissingField{
				Key:    key,
				Reason: "required config key not set",
			})
		}
	}
	return missing
}

// isEmptyValue reports whether v should be treated as "not actually set"
// even though the key is present — e.g. an explicit empty string.
func isEmptyValue(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return val == ""
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	default:
		return false
	}
}

// FormatMissing renders missing as an actionable multi-line exit message.
// Returns "" when missing is empty.
func FormatMissing(missing []MissingField) string {
	if len(missing) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("missing required config:\n")
	for _, m := range missing {
		b.WriteString("  - ")
		b.WriteString(m.Key)
		b.WriteString(" (")
		b.WriteString(m.Reason)
		b.WriteString("); define it via --config-file, --user-config-file, or the init wizard\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
