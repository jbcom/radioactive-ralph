package ipc

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"runtime"
)

// maxUnixSocketPath is the conservative upper bound on a Unix-domain socket
// path length. The kernel's sockaddr_un.sun_path is 104 bytes on the BSDs
// (including macOS) and 108 on Linux; binding a longer path fails with
// "invalid argument" (EINVAL). We use the smaller 104 limit everywhere and
// leave a byte for the NUL terminator so the same fallback triggers on every
// POSIX host — a socket path that is safe on macOS is safe on Linux too.
const maxUnixSocketPath = 103

// ServiceEndpoint returns the local control-plane endpoint plus its heartbeat
// file for one repo workspace.
//
// On POSIX the endpoint is normally sessionsDir/service.sock. But a deeply
// nested sessionsDir — a long XDG/App Support path, a deep RALPH_STATE_DIR,
// or a macOS /var/folders/... temp root under test — can push that path past
// the kernel's sun_path limit, so bind() fails with EINVAL. When that would
// happen we fall back to a short, collision-resistant socket path under the
// system temp dir keyed by a hash of sessionsDir. The heartbeat file always
// stays in sessionsDir (it is a plain file, not a socket, so it has no path
// limit) which keeps discovery/liveness colocated with the workspace.
func ServiceEndpoint(sessionsDir string) (endpoint, heartbeat string) {
	return serviceEndpointForGOOS(runtime.GOOS, os.TempDir(), sessionsDir)
}

func serviceEndpointForGOOS(goos, tempDir, sessionsDir string) (endpoint, heartbeat string) {
	token := endpointToken(sessionsDir)
	if goos == "windows" {
		heartbeat = filepath.Join(sessionsDir, "service.alive")
		return `\\.\pipe\radioactive_ralph-` + token + `-service`, heartbeat
	}

	heartbeat = path.Join(sessionsDir, "service.sock.alive")
	natural := path.Join(sessionsDir, "service.sock")
	if len(natural) <= maxUnixSocketPath {
		return natural, heartbeat
	}
	// Natural path is too long for sun_path: bind it under the system temp
	// dir with a short hashed name instead. Deterministic in sessionsDir so
	// the supervisor and every client resolve the identical socket.
	short := filepath.Join(tempDir, "rralph-"+token+".sock")
	return short, heartbeat
}

// endpointToken is a short, stable, collision-resistant token derived from
// sessionsDir, used to name the Windows pipe and the POSIX short-path socket.
func endpointToken(sessionsDir string) string {
	sum := sha256.Sum256([]byte(sessionsDir))
	return hex.EncodeToString(sum[:])[:12]
}
