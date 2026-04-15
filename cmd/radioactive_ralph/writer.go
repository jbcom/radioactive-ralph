package main

import (
	"io"
	"os"
)

// newStdoutWriter returns os.Stdout typed as io.Writer. Extracted to
// a named helper so tests can swap it for a bytes.Buffer.
func newStdoutWriter() io.Writer { return os.Stdout }
