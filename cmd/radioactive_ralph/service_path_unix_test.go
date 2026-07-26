//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import "testing"

func TestUnixInferredServicePathAllowedValidatesEveryComponent(t *testing.T) {
	const effectiveUID = 501
	trusted := map[string]servicePathMetadata{
		"/": {
			mode:      0o755,
			uid:       0,
			directory: true,
		},
		"/safe": {
			mode:      0o755,
			uid:       0,
			directory: true,
		},
		"/safe/user": {
			mode:      0o700,
			uid:       effectiveUID,
			directory: true,
		},
		"/safe/user/bin": {
			mode:      0o755,
			uid:       effectiveUID,
			directory: true,
		},
	}
	allowed := func(
		candidate string,
		overrides map[string]servicePathMetadata,
		unavailable map[string]bool,
	) bool {
		return unixInferredServicePathAllowed(
			candidate,
			effectiveUID,
			func(path string) (servicePathMetadata, bool) {
				if unavailable[path] {
					return servicePathMetadata{}, false
				}
				if override, ok := overrides[path]; ok {
					return override, true
				}
				metadata, ok := trusted[path]
				return metadata, ok
			},
		)
	}

	if !allowed("/safe/user/bin", nil, nil) {
		t.Fatal("trusted component chain rejected")
	}
	if allowed("safe/user/bin", nil, nil) {
		t.Fatal("relative path accepted")
	}
	if allowed("/safe/user/bin", map[string]servicePathMetadata{
		"/safe": {
			mode:      0o777,
			uid:       0,
			directory: true,
		},
	}, nil) {
		t.Fatal("world-writable ancestor accepted")
	}
	if allowed("/safe/user/bin", map[string]servicePathMetadata{
		"/safe/user": {
			mode:      0o700,
			uid:       effectiveUID,
			directory: true,
			symlink:   true,
		},
	}, nil) {
		t.Fatal("symlinked ancestor accepted")
	}
	if allowed("/safe/user/bin", map[string]servicePathMetadata{
		"/safe": {
			mode:      0o755,
			uid:       502,
			directory: true,
		},
	}, nil) {
		t.Fatal("foreign-owned ancestor accepted")
	}
	if allowed(
		"/safe/user/bin",
		nil,
		map[string]bool{"/safe/user": true},
	) {
		t.Fatal("missing ancestor accepted")
	}
}
