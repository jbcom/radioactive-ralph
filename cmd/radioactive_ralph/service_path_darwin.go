//go:build darwin

package main

import (
	"runtime"

	"golang.org/x/sys/unix"
)

const (
	darwinArmHomebrewBin   = "/opt/homebrew/bin"
	darwinIntelHomebrewBin = "/usr/local/bin"
	darwinWheelGroup       = 0
	darwinAdminGroup       = 80 // macOS's privileged admin group ABI.
)

func platformInferredServicePathAllowed(
	candidate string,
	effectiveUID uint32,
) bool {
	return darwinHomebrewPathAllowed(
		candidate,
		currentDarwinHostArchitecture(),
		effectiveUID,
		readServicePathMetadata,
	)
}

func currentDarwinHostArchitecture() string {
	return darwinHostArchitecture(
		runtime.GOARCH,
		func() (uint32, error) {
			return unix.SysctlUint32("sysctl.proc_translated")
		},
	)
}

func darwinHostArchitecture(
	processArchitecture string,
	processTranslated func() (uint32, error),
) string {
	if processArchitecture != "amd64" {
		return processArchitecture
	}
	translated, err := processTranslated()
	if err == nil && translated == 1 {
		return "arm64"
	}
	return processArchitecture
}

func darwinHomebrewPathAllowed(
	candidate string,
	architecture string,
	effectiveUID uint32,
	metadata func(string) (servicePathMetadata, bool),
) bool {
	root, ok := metadata("/")
	if !ok || !trustedDarwinRoot(root) {
		return false
	}

	switch {
	case architecture == "arm64" && candidate == darwinArmHomebrewBin:
		return darwinArmHomebrewPathAllowed(
			effectiveUID,
			metadata,
		)
	case architecture == "amd64" && candidate == darwinIntelHomebrewBin:
		return darwinIntelHomebrewPathAllowed(
			effectiveUID,
			metadata,
		)
	default:
		return false
	}
}

func darwinArmHomebrewPathAllowed(
	effectiveUID uint32,
	metadata func(string) (servicePathMetadata, bool),
) bool {
	opt, ok := metadata("/opt")
	if !ok ||
		!trustedDarwinStrictDirectory(opt, 0, 0) ||
		opt.gid != darwinWheelGroup {
		return false
	}
	prefix, ok := metadata("/opt/homebrew")
	if !ok ||
		!trustedDarwinStrictDirectory(prefix, 0, effectiveUID) ||
		(prefix.gid != darwinWheelGroup &&
			prefix.gid != darwinAdminGroup) {
		return false
	}
	bin, ok := metadata(darwinArmHomebrewBin)
	return ok &&
		bin.directory &&
		!bin.symlink &&
		(bin.uid == 0 || bin.uid == effectiveUID) &&
		bin.gid == darwinAdminGroup &&
		bin.mode.Perm()&0o002 == 0
}

func darwinIntelHomebrewPathAllowed(
	effectiveUID uint32,
	metadata func(string) (servicePathMetadata, bool),
) bool {
	usr, ok := metadata("/usr")
	if !ok ||
		!trustedDarwinStrictDirectory(usr, 0, 0) ||
		usr.gid != darwinWheelGroup {
		return false
	}
	prefix, ok := metadata("/usr/local")
	if !ok || !trustedDarwinPrivilegedHomebrewDirectory(prefix, effectiveUID) {
		return false
	}
	bin, ok := metadata(darwinIntelHomebrewBin)
	return ok && trustedDarwinPrivilegedHomebrewDirectory(bin, effectiveUID)
}

func trustedDarwinRoot(metadata servicePathMetadata) bool {
	return trustedDarwinStrictDirectory(metadata, 0, 0) &&
		metadata.gid == darwinWheelGroup
}

func trustedDarwinStrictDirectory(
	metadata servicePathMetadata,
	rootUID, effectiveUID uint32,
) bool {
	return metadata.directory &&
		!metadata.symlink &&
		(metadata.uid == rootUID || metadata.uid == effectiveUID) &&
		metadata.mode.Perm()&0o022 == 0
}

func trustedDarwinPrivilegedHomebrewDirectory(
	metadata servicePathMetadata,
	effectiveUID uint32,
) bool {
	if !metadata.directory ||
		metadata.symlink ||
		(metadata.uid != 0 && metadata.uid != effectiveUID) ||
		metadata.mode.Perm()&0o002 != 0 {
		return false
	}
	if metadata.mode.Perm()&0o020 != 0 {
		return metadata.gid == darwinAdminGroup
	}
	return metadata.gid == darwinWheelGroup ||
		metadata.gid == darwinAdminGroup
}
