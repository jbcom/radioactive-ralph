package main

import "testing"

func TestAddrFromURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:7777/mcp":              "localhost:7777",
		"https://user:pass@example.com:8443/mcp": "example.com:8443",
		"http://":                                "",
		"localhost:7777/mcp":                     "localhost:7777",
	}
	for in, want := range cases {
		if got := addrFromURL(in); got != want {
			t.Errorf("addrFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
