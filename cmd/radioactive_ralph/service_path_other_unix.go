//go:build aix || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

func platformInferredServicePathAllowed(string, uint32) bool {
	return false
}
