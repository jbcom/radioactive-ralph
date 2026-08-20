//go:build darwin || linux

package agent

import (
	"fmt"
	"strings"
	"time"
)

const processScopePollInterval = 10 * time.Millisecond

// ReclaimProcessScope kills and proves absent every same-user process carrying
// the exact environment scope inherited from a managed provider launch. Unlike
// process groups and sessions, the scope survives setpgid(2), setsid(2), and
// reparenting. Platform implementations acquire a stable kernel handle before
// signalling so a recycled numeric PID cannot target an unrelated process.
//
// The scope value is intentionally never included in an error. Callers must use
// a cryptographically random, per-launch value and must not serialize it.
func ReclaimProcessScope(envKey, envValue string, excludedPID int, timeout time.Duration) error {
	if envKey == "" || strings.ContainsAny(envKey, "=\x00") || envValue == "" || strings.ContainsRune(envValue, '\x00') {
		return fmt.Errorf("agent: invalid process scope")
	}
	if excludedPID <= 1 || timeout <= 0 {
		return fmt.Errorf("agent: invalid process scope boundary")
	}
	return reclaimProcessScope(envKey+"="+envValue, excludedPID, timeout)
}
