package provider

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	maxAuthoritativeResultBytes = 16 << 20
	maxStructuredEvidenceBytes  = 16 << 20
)

var (
	// ErrAuthoritativeResultTooLarge is static because provider-controlled
	// result bytes must never cross into an error path.
	ErrAuthoritativeResultTooLarge = errors.New("provider: authoritative result exceeded 16MiB limit")

	// ErrStructuredEvidenceTooLarge applies to the Ralph-owned JSONL tee used
	// by stream providers. It is independent of the Agent's observational
	// per-line retention budget.
	ErrStructuredEvidenceTooLarge = errors.New("provider: structured evidence exceeded 16MiB limit")

	// ErrAuthoritativeResultUnsafe means the provider result path did not
	// resolve to the same regular file before and after a non-following,
	// nonblocking open. It is static so path or provider-controlled bytes never
	// cross the error boundary.
	ErrAuthoritativeResultUnsafe = errors.New("provider: authoritative result was not an identity-stable regular file")
)

type boundedResultBuffer struct {
	buf bytes.Buffer
	n   int
}

func (b *boundedResultBuffer) writeString(value string) error {
	return b.writeStringReserved(value, 0)
}

func (b *boundedResultBuffer) writeStringReserved(value string, reserved int) error {
	if reserved < 0 || reserved > maxAuthoritativeResultBytes-b.n ||
		len(value) > maxAuthoritativeResultBytes-b.n-reserved {
		return ErrAuthoritativeResultTooLarge
	}
	_, _ = b.buf.WriteString(value)
	b.n += len(value)
	return nil
}

func (b *boundedResultBuffer) String() string {
	return b.buf.String()
}

type boundedEvidenceFile struct {
	file *os.File
	n    int
}

func newBoundedEvidenceFile(path string) (*boundedEvidenceFile, error) {
	file, err := os.Create(path) //nolint:gosec // Ralph-owned temp file
	if err != nil {
		return nil, err
	}
	return &boundedEvidenceFile{file: file}, nil
}

func (f *boundedEvidenceFile) writeFrame(frame []byte) error {
	if len(frame) > maxStructuredEvidenceBytes-f.n {
		return ErrStructuredEvidenceTooLarge
	}
	n, err := f.file.Write(frame)
	f.n += n
	if err != nil {
		return fmt.Errorf("provider: write structured evidence: %w", err)
	}
	if n != len(frame) {
		return fmt.Errorf("provider: write structured evidence: %w", io.ErrShortWrite)
	}
	return nil
}

func (f *boundedEvidenceFile) close() error {
	if f == nil || f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	if err != nil {
		return fmt.Errorf("provider: close structured evidence: %w", err)
	}
	return nil
}

func readBoundedAuthoritativeResult(path string) ([]byte, error) {
	return readBoundedAuthoritativeResultWithOpener(path, openAuthoritativeResultFile)
}

type authoritativeResultOpener func(string) (*os.File, error)

func readBoundedAuthoritativeResultWithOpener(
	path string,
	openFile authoritativeResultOpener,
) ([]byte, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, ErrAuthoritativeResultUnsafe
	}
	if pathInfo.Size() > maxAuthoritativeResultBytes {
		return nil, ErrAuthoritativeResultTooLarge
	}

	// Platform openers reject final-component symlinks and cannot block on a
	// FIFO/device substitution. The pre/post identity comparison rejects a
	// regular-file swap in the Lstat-to-open window.
	file, err := openFile(path)
	if err != nil {
		return nil, errors.Join(ErrAuthoritativeResultUnsafe, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
		return nil, ErrAuthoritativeResultUnsafe
	}
	if info.Size() > maxAuthoritativeResultBytes {
		return nil, ErrAuthoritativeResultTooLarge
	}

	raw, err := io.ReadAll(io.LimitReader(file, maxAuthoritativeResultBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxAuthoritativeResultBytes {
		return nil, ErrAuthoritativeResultTooLarge
	}
	return raw, nil
}
