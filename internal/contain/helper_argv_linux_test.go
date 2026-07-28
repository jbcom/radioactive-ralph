//go:build linux

package contain

import "testing"

// TestHelperArgvRoundTripsExtraPaths pins the LINUX helper protocol, where the
// grant list crosses a process boundary as argv.
//
// The extras are length-PREFIXED rather than delimited. A path is arbitrary
// text, so any sentinel token could legitimately appear as a directory name and
// would silently truncate the list -- producing a contained turn with the WRONG
// boundary rather than an error. A count cannot collide with its own data.
//
// This is the darwin-side unit of a cross-platform contract: the linux CI job
// exercises it end to end, and this catches a format change locally first.
func TestHelperArgvRoundTripsExtraPaths(t *testing.T) {
	root := t.TempDir()
	a, b := t.TempDir(), t.TempDir()
	p, err := NewPolicy(root, a, b)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if len(p.ExtraWritable) != 2 {
		t.Fatalf("ExtraWritable = %v, want two entries", p.ExtraWritable)
	}
	// Reconstruct the argv the linux wrapper builds, then parse it back.
	argv := append([]string{"ralph", helperFlag, p.Root, "2"}, p.ExtraWritable...)
	argv = append(argv, "/bin/true", "--flag")

	gotRoot, gotExtra, gotCmd, ok := parseHelperInvocation(argv)
	if !ok {
		t.Fatalf("parseHelperInvocation refused a well-formed argv: %v", argv)
	}
	if gotRoot != p.Root {
		t.Errorf("root = %q, want %q", gotRoot, p.Root)
	}
	if len(gotExtra) != 2 || gotExtra[0] != p.ExtraWritable[0] || gotExtra[1] != p.ExtraWritable[1] {
		t.Errorf("extra = %v, want %v", gotExtra, p.ExtraWritable)
	}
	if len(gotCmd) != 2 || gotCmd[0] != "/bin/true" {
		t.Errorf("command = %v, want [/bin/true --flag]", gotCmd)
	}
}

// TestHelperArgvRejectsAMalformedCount keeps a corrupt invocation from being
// interpreted as a SHORTER grant list, which would run the provider under a
// boundary nobody chose.
func TestHelperArgvRejectsAMalformedCount(t *testing.T) {
	for name, argv := range map[string][]string{
		"non-numeric count":  {"ralph", helperFlag, "/root", "two", "/a", "/bin/true"},
		"count exceeds argv": {"ralph", helperFlag, "/root", "5", "/a", "/bin/true"},
		"negative count":     {"ralph", helperFlag, "/root", "-1", "/bin/true"},
		"no command":         {"ralph", helperFlag, "/root", "1", "/a"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, ok := parseHelperInvocation(argv); ok {
				t.Fatalf("accepted a malformed helper argv %v; a misparsed count "+
					"silently changes which paths are writable", argv)
			}
		})
	}
}
