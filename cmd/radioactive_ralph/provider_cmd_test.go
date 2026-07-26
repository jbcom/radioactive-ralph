package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

func TestCalibrationCommandsRequireSupervisorAndNeverOpenStoreDirectly(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "import",
			run: func() error {
				_, err := putCalibration(context.Background(), ipc.CalibrationRecord{})
				return err
			},
		},
		{
			name: "show",
			run: func() error {
				_, err := getCalibrations(context.Background(), "sha256:test")
				return err
			},
		},
		{
			name: "list",
			run: func() error {
				_, err := getCalibrations(context.Background(), "")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if !errors.Is(err, supervisor.ErrNoSupervisor) {
				t.Fatalf("error = %v, want ErrNoSupervisor", err)
			}
			if _, statErr := os.Stat(storeDBPath(stateDir)); !os.IsNotExist(statErr) {
				t.Fatalf("calibration command opened store without supervisor: stat error = %v", statErr)
			}
		})
	}
}
