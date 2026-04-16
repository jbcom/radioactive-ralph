package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// realRunner is the production exec.CommandContext runner.
func realRunner(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // name + args are hardcoded strings from the check list
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w (%s)", name, err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// withTimeout runs fn with a per-call timeout, returning the result.
func withTimeout(parent context.Context, d time.Duration, fn func(context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	out, err := fn(ctx)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("timeout: %w", err)
	}
	return out, err
}

// parseVersion extracts the dotted version number from a `--version`
// output. Most CLIs emit "foo version X.Y.Z ..." or "X.Y.Z"; we handle
// both shapes by stripping an optional prefix and taking the first
// dotted token.
func parseVersion(out, prefix string) string {
	trimmed := strings.TrimSpace(out)
	if prefix != "" {
		trimmed = strings.TrimPrefix(trimmed, prefix)
		trimmed = strings.TrimSpace(trimmed)
	}
	// Take the first whitespace-separated token and strip trailing
	// non-version chars (e.g. "2.42.0.1").
	for tok := range strings.FieldsSeq(trimmed) {
		if looksLikeVersion(tok) {
			return tok
		}
	}
	return trimmed
}

// looksLikeVersion reports whether s looks like a dotted numeric
// version (at least one dot, numeric first segment).
func looksLikeVersion(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	// First part must be digits.
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// versionAtLeast reports whether got >= want using simple dotted-int
// comparison. Non-numeric suffixes (e.g. "2.42.0-beta") are tolerated:
// we split on dots, compare numeric prefixes, shorter versions are
// treated as if padded with zeros.
func versionAtLeast(got, want string) bool {
	gotParts := versionParts(got)
	wantParts := versionParts(want)
	for i := range wantParts {
		var g int
		if i < len(gotParts) {
			g = gotParts[i]
		}
		w := wantParts[i]
		if g > w {
			return true
		}
		if g < w {
			return false
		}
	}
	return true
}

// versionParts splits s on dots and returns the numeric prefix of each
// segment as an int. Non-numeric suffixes are truncated at the first
// non-digit character.
func versionParts(s string) []int {
	parts := make([]int, 0, 4)
	for seg := range strings.SplitSeq(s, ".") {
		n := 0
		for _, r := range seg {
			if r >= '0' && r <= '9' {
				n = n*10 + int(r-'0')
			} else {
				break
			}
		}
		parts = append(parts, n)
	}
	return parts
}
