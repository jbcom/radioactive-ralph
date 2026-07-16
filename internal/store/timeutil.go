package store

import "time"

// parseDBTimestamp parses a SQLite DATETIME column value written by
// CURRENT_TIMESTAMP or by our own RFC3339 writes, trying every layout we
// might encounter.
func parseDBTimestamp(raw string) time.Time {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05.999999999Z07:00",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// isUniqueViolation recognizes the modernc.org/sqlite UNIQUE error. Keeps
// the error matching in one place so callers can rely on a sentinel error
// unconditionally.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite error text is stable across versions.
	return containsAny(err.Error(), "UNIQUE constraint", "constraint failed: UNIQUE")
}

// containsAny is a case-sensitive multi-needle substring check.
func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		for i := 0; i+len(n) <= len(haystack); i++ {
			if haystack[i:i+len(n)] == n {
				return true
			}
		}
	}
	return false
}

// nullIfEmpty converts an empty string to a SQL NULL binding.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// statusPlaceholders returns "?,?,...,?" with n placeholders.
func statusPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
