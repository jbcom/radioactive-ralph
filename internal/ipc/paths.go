package ipc

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"runtime"
)

// ServiceEndpoint returns the local control-plane endpoint plus its heartbeat
// file for one repo workspace.
func ServiceEndpoint(sessionsDir string) (endpoint, heartbeat string) {
	return serviceEndpointForGOOS(runtime.GOOS, sessionsDir)
}

func serviceEndpointForGOOS(goos, sessionsDir string) (endpoint, heartbeat string) {
	if goos == "windows" {
		heartbeat = filepath.Join(sessionsDir, "service.alive")
		sum := sha256.Sum256([]byte(sessionsDir))
		token := hex.EncodeToString(sum[:])[:12]
		return `\\.\pipe\radioactive_ralph-` + token + `-service`, heartbeat
	}
	endpoint = path.Join(sessionsDir, "service.sock")
	return endpoint, endpoint + ".alive"
}
