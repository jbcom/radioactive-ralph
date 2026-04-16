//go:build windows

package ipc

import "testing"

func TestPipeSecurityDescriptorForCurrentUser(t *testing.T) {
	got := pipeSecurityDescriptorForSID("S-1-5-21-1000", false)
	want := "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;S-1-5-21-1000)"
	if got != want {
		t.Fatalf("descriptor = %q, want %q", got, want)
	}
}

func TestPipeSecurityDescriptorForLocalSystem(t *testing.T) {
	got := pipeSecurityDescriptorForSID("S-1-5-18", true)
	want := "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)"
	if got != want {
		t.Fatalf("descriptor = %q, want %q", got, want)
	}
}
