package ipc

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
)

// ServiceEndpoint returns the local control-plane endpoint plus its heartbeat
// file for one repo workspace.
func ServiceEndpoint(sessionsDir string) (endpoint, heartbeat string) {
	return serviceEndpointForGOOS(runtime.GOOS, sessionsDir)
}

func serviceEndpointForGOOS(goos, sessionsDir string) (endpoint, heartbeat string) {
	heartbeat = filepath.Join(sessionsDir, "service.alive")
	if goos == "windows" {
		sum := sha256.Sum256([]byte(sessionsDir))
		token := hex.EncodeToString(sum[:])[:12]
		return `\\.\pipe\radioactive_ralph-` + token + `-service`, heartbeat
	}
	endpoint = filepath.Join(sessionsDir, "service.sock")
	return endpoint, endpoint + ".alive"
}
