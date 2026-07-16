package supervisor

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readPIDFile reads the PID written by acquirePIDLock (a single integer on
// the first line) from path. Returns 0, nil if the file does not exist or
// is empty — callers treat that as "no recorded PID" rather than an error.
func readPIDFile(path string) (int, error) {
	f, err := os.Open(path) //nolint:gosec // supervisor-owned path
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open pid file: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0, nil
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return 0, nil
	}
	pid, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("parse pid file %s: %w", path, err)
	}
	return pid, nil
}
