//go:build !darwin && !linux && !windows

package provider

import (
	"errors"
	"os"
)

var errAuthoritativeResultOpenUnsupported = errors.New("provider: safe authoritative result open unsupported on this platform")

func openAuthoritativeResultFile(string) (*os.File, error) {
	return nil, errAuthoritativeResultOpenUnsupported
}
