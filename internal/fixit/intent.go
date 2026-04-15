package fixit

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IntentOptions feeds CaptureIntent.
type IntentOptions struct {
	Topic          string
	Description    string
	Constraints    []string
	NonInteractive bool
	RepoRoot       string
	Stdin          io.Reader // defaults to os.Stdin
	Stdout         io.Writer // defaults to os.Stdout
}

// CaptureIntent runs Stage 1. In non-interactive mode it returns
// immediately with whatever the caller supplied. In interactive mode
// it asks three short questions on stdout and reads answers from
// stdin. Either way it consults a TOPIC.md at the repo root for an
// operator-prepared description if --description wasn't passed.
func CaptureIntent(opts IntentOptions) (IntentSpec, error) {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}

	spec := IntentSpec{
		Topic:       sanitizeTopic(opts.Topic),
		Description: opts.Description,
		Constraints: append([]string(nil), opts.Constraints...),
		AnswersToQs: map[string]string{},
	}

	// Fall back to TOPIC.md when --description is empty. This is the
	// "operator pre-wrote a brief" path that lets fixit run without
	// any flags in shell scripts.
	if spec.Description == "" && opts.RepoRoot != "" {
		topicMD := filepath.Join(opts.RepoRoot, "TOPIC.md")
		if raw, err := os.ReadFile(topicMD); err == nil { //nolint:gosec // operator-controlled path
			spec.Description = strings.TrimSpace(string(raw))
		}
	}

	if opts.NonInteractive {
		return spec, nil
	}

	// Interactive Q&A — three short questions. Each question the
	// operator answers becomes a constraint or augments Description.
	reader := bufio.NewReader(opts.Stdin)

	if spec.Description == "" {
		_, _ = fmt.Fprintln(opts.Stdout, "Fixit: what's the goal?")
		_, _ = fmt.Fprint(opts.Stdout, "  > ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return spec, fmt.Errorf("read goal: %w", err)
		}
		spec.Description = strings.TrimSpace(line)
		spec.AnswersToQs["goal"] = spec.Description
	}

	_, _ = fmt.Fprintln(opts.Stdout, "Fixit: any variants off-limits? (comma-separated names, blank for none)")
	_, _ = fmt.Fprint(opts.Stdout, "  > ")
	line, _ := reader.ReadString('\n')
	if banned := strings.TrimSpace(line); banned != "" {
		for _, name := range strings.Split(banned, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				spec.Constraints = append(spec.Constraints,
					"variant off-limits: "+name)
			}
		}
		spec.AnswersToQs["banned_variants"] = banned
	}

	_, _ = fmt.Fprintln(opts.Stdout, "Fixit: time/budget cap? (single-session | hour | days | week | none)")
	_, _ = fmt.Fprint(opts.Stdout, "  > ")
	line, _ = reader.ReadString('\n')
	if budgetCap := strings.TrimSpace(line); budgetCap != "" {
		spec.Constraints = append(spec.Constraints, "time/budget cap: "+budgetCap)
		spec.AnswersToQs["cap"] = budgetCap
	}

	return spec, nil
}

// sanitizeTopic returns a lowercase slug safe for filenames on every
// supported platform. Mirrors cmd/ralph/advisor.go::sanitizeTopic so
// the two callers can agree on the output filename.
func sanitizeTopic(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "general"
	}
	var b strings.Builder
	lastSep := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastSep = false
		case r == '-' || r == '_':
			if !lastSep {
				b.WriteRune(r)
				lastSep = true
			}
		default:
			if !lastSep {
				b.WriteRune('-')
				lastSep = true
			}
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "general"
	}
	return out
}
