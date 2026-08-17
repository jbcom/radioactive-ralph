//go:build !darwin && !linux && !windows

package adapters

import (
	"errors"
	"os"
)

var errReleaseOpenUnsupported = errors.New("adapters: safe release verification unsupported on this platform")

func openReleaseFile(string) (*os.File, error) {
	return nil, errReleaseOpenUnsupported
}
