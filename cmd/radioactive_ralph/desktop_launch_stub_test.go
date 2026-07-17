//go:build !gui

package main

import (
	"context"
	"testing"
)

// TestMaybeLaunchDesktopGUI_StubIsNoop confirms the default (non-GUI) build's
// hook never claims to handle the launch, so a bare invocation always falls
// through to the client path.
func TestMaybeLaunchDesktopGUI_StubIsNoop(t *testing.T) {
	handled, err := maybeLaunchDesktopGUI(context.Background(), nil)
	if handled {
		t.Error("non-GUI stub returned handled=true, want false")
	}
	if err != nil {
		t.Errorf("non-GUI stub returned err=%v, want nil", err)
	}
}
