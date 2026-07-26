package main

import (
	"os"
	"testing"
)

func TestGenericInferredServicePathAllowed(t *testing.T) {
	const effectiveUID = 501
	tests := []struct {
		name     string
		metadata servicePathMetadata
		want     bool
	}{
		{
			name: "root owned directory",
			metadata: servicePathMetadata{
				mode:      0o755,
				uid:       0,
				directory: true,
			},
			want: true,
		},
		{
			name: "effective user owned private directory",
			metadata: servicePathMetadata{
				mode:      0o700,
				uid:       effectiveUID,
				directory: true,
			},
			want: true,
		},
		{
			name: "effective user owned readable directory",
			metadata: servicePathMetadata{
				mode:      0o755,
				uid:       effectiveUID,
				directory: true,
			},
			want: true,
		},
		{
			name: "foreign owned directory",
			metadata: servicePathMetadata{
				mode:      0o755,
				uid:       502,
				directory: true,
			},
		},
		{
			name: "effective user group writable directory",
			metadata: servicePathMetadata{
				mode:      0o775,
				uid:       effectiveUID,
				directory: true,
			},
		},
		{
			name: "root owned world writable directory",
			metadata: servicePathMetadata{
				mode:      0o777,
				uid:       0,
				directory: true,
			},
		},
		{
			name: "regular file",
			metadata: servicePathMetadata{
				mode: os.FileMode(0o755),
				uid:  effectiveUID,
			},
		},
		{
			name: "symlink",
			metadata: servicePathMetadata{
				mode:      os.ModeSymlink | 0o755,
				uid:       effectiveUID,
				directory: true,
				symlink:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := genericInferredServicePathAllowed(tt.metadata, effectiveUID); got != tt.want {
				t.Fatalf("genericInferredServicePathAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
