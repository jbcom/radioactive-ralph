package adapters

import (
	"fmt"
	"io"
	"time"
)

type openCodeVerificationProgress struct {
	stopCh chan struct{}
	doneCh chan struct{}
}

// startOpenCodeVerificationProgress keeps the parent provider's renewable
// stall lease alive while the supervisor runs a bounded acceptance check. The
// line is static and carries no provider input, path, status detail, or secret.
func startOpenCodeVerificationProgress(
	output io.Writer, interval time.Duration,
) *openCodeVerificationProgress {
	_, _ = fmt.Fprintln(output, managedOpenCodeVerificationWait)
	progress := &openCodeVerificationProgress{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	go func() {
		defer close(progress.doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = fmt.Fprintln(output, managedOpenCodeVerificationWait)
			case <-progress.stopCh:
				return
			}
		}
	}()
	return progress
}

func (p *openCodeVerificationProgress) stop() {
	close(p.stopCh)
	<-p.doneCh
}
