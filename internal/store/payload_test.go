package store

import (
	"encoding/json"
	"testing"
)

func TestJSONOrEmptyObject(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty is empty object", "", "{}"},
		{"valid object passes through", `{"a":1}`, `{"a":1}`},
		{"valid array passes through", `[1,2,3]`, `[1,2,3]`},
		{"valid string literal passes through", `"hello"`, `"hello"`},
		{"malformed is wrapped, not corrupt", `{not json`, `{"raw":"{not json"}`},
		{"bare word is wrapped", `oops`, `{"raw":"oops"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := jsonOrEmptyObject(tc.in)
			if got != tc.want {
				t.Errorf("jsonOrEmptyObject(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// The invariant: the result is ALWAYS valid JSON, whatever the input.
			if !json.Valid([]byte(got)) {
				t.Errorf("jsonOrEmptyObject(%q) = %q, which is not valid JSON", tc.in, got)
			}
		})
	}
}
