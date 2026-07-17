package onboard

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// StdinPrompter is the real Prompter: it writes the question to Out and reads
// one line from In, interpreting y/yes → true, n/no → false, q/quit →
// ErrQuit, and an empty line → the supplied default. Any other input re-asks
// (bounded, so a closed stdin doesn't loop forever).
type StdinPrompter struct {
	In  io.Reader
	Out io.Writer

	reader *bufio.Reader
}

// NewStdinPrompter builds a StdinPrompter over in/out.
func NewStdinPrompter(in io.Reader, out io.Writer) *StdinPrompter {
	return &StdinPrompter{In: in, Out: out, reader: bufio.NewReader(in)}
}

// maxPromptRetries bounds re-asks on unrecognized input so a piped/closed
// stdin can't spin forever (it also can't reach here — the caller gates on an
// interactive TTY — but defense in depth is cheap).
const maxPromptRetries = 3

// Confirm implements Prompter.
func (p *StdinPrompter) Confirm(question string, defaultYes bool) (bool, error) {
	suffix := "[y/N/q]"
	if defaultYes {
		suffix = "[Y/n/q]"
	}
	for attempt := 0; attempt < maxPromptRetries; attempt++ {
		_, _ = fmt.Fprintf(p.Out, "%s %s ", question, suffix)
		line, err := p.reader.ReadString('\n')
		if err != nil && line == "" {
			// EOF/closed stdin with nothing typed: treat as quit so we fall
			// back to the safe manual path rather than erroring hard.
			return false, ErrQuit
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "q", "quit":
			return false, ErrQuit
		default:
			_, _ = fmt.Fprintln(p.Out, "Please answer y, n, or q.")
		}
	}
	// Too many unrecognized answers — fall back to the safe path.
	return false, ErrQuit
}
