package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureAliveMissingUnixEndpoint(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "service.sock")
	heartbeat := socket + ".alive"
	err := ensureAlive(socket, heartbeat)
	if err == nil {
		t.Fatal("ensureAlive() unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "no repo service endpoint") {
		t.Fatalf("ensureAlive() = %v", err)
	}
}

func TestEnsureAliveWindowsPipeWithFreshHeartbeat(t *testing.T) {
	dir := t.TempDir()
	heartbeat := filepath.Join(dir, "service.alive")
	if err := os.WriteFile(heartbeat, []byte("ok"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	socket := `\\.\pipe\radioactive_ralph-test-service`
	if err := ensureAlive(socket, heartbeat); err != nil {
		t.Fatalf("ensureAlive() = %v", err)
	}
}

func TestEnsureAliveWindowsPipeWithMissingHeartbeat(t *testing.T) {
	heartbeat := filepath.Join(t.TempDir(), "missing.alive")
	socket := `\\.\pipe\radioactive_ralph-test-service`
	err := ensureAlive(socket, heartbeat)
	if err == nil {
		t.Fatal("ensureAlive() unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "no repo service heartbeat") {
		t.Fatalf("ensureAlive() = %v", err)
	}
}

func TestEnsureAliveUnixEndpointWithStaleHeartbeat(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "service.sock")
	heartbeat := socket + ".alive"
	if err := os.WriteFile(socket, []byte("socket"), 0o600); err != nil {
		t.Fatalf("WriteFile socket: %v", err)
	}
	if err := os.WriteFile(heartbeat, []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile heartbeat: %v", err)
	}
	old := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(heartbeat, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	err := ensureAlive(socket, heartbeat)
	if err == nil {
		t.Fatal("ensureAlive() unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "heartbeat is stale") {
		t.Fatalf("ensureAlive() = %v", err)
	}
}
