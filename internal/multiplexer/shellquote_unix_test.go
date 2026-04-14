//go:build unix

package multiplexer

import "testing"

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/tmp/log":            "'/tmp/log'",
		"/tmp/with space/log": "'/tmp/with space/log'",
		"/tmp/with'quote":     `'/tmp/with'\''quote'`,
		"":                    "''",
	}
	for in, want := range cases {
		got := shellQuote(in)
		if got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
