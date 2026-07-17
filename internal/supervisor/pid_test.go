package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPIDFileMissingIsZeroNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.pid")
	pid, err := readPIDFile(path)
	if err != nil {
		t.Fatalf("readPIDFile(missing): %v", err)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

func TestReadPIDFileEmptyIsZeroNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pid")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	pid, err := readPIDFile(path)
	if err != nil {
		t.Fatalf("readPIDFile(empty): %v", err)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

func TestReadPIDFileBlankLineIsZeroNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blank.pid")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	pid, err := readPIDFile(path)
	if err != nil {
		t.Fatalf("readPIDFile(blank line): %v", err)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

func TestReadPIDFileValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "valid.pid")
	if err := os.WriteFile(path, []byte("12345\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	pid, err := readPIDFile(path)
	if err != nil {
		t.Fatalf("readPIDFile: %v", err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
}

func TestReadPIDFileMalformedIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed.pid")
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := readPIDFile(path); err == nil {
		t.Error("readPIDFile(malformed): want error, got nil")
	}
}

func TestPidAliveRejectsNonPositive(t *testing.T) {
	if pidAlive(0) {
		t.Error("pidAlive(0) = true, want false")
	}
	if pidAlive(-1) {
		t.Error("pidAlive(-1) = true, want false")
	}
}

func TestPidAliveSelf(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("pidAlive(own pid) = false, want true")
	}
}

func TestPidAliveDeadPID(t *testing.T) {
	// PID 1 is real (init/launchd) and always alive on any Unix host;
	// use a PID far above any realistic live range instead, which is
	// vanishingly unlikely to collide with a real running process.
	if pidAlive(1 << 30) {
		t.Error("pidAlive(implausibly large pid) = true, want false")
	}
}
