package main

import "os"

type servicePathMetadata struct {
	mode      os.FileMode
	uid       uint32
	gid       uint32
	directory bool
	symlink   bool
}

func genericInferredServicePathAllowed(
	metadata servicePathMetadata,
	effectiveUID uint32,
) bool {
	if !metadata.directory || metadata.symlink ||
		metadata.mode.Perm()&0o022 != 0 {
		return false
	}
	return metadata.uid == 0 || metadata.uid == effectiveUID
}
