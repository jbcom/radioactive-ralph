package provider

import (
	"context"
	"io"
	"sync"
	"time"
)

// withProgressLease returns a child context canceled with ErrProviderStalled
// when no stdout/stderr bytes are observed before timeout. Progress renews only
// this lease; it never changes the parent turn deadline.
func withProgressLease(parent context.Context, timeout time.Duration) (context.Context, func(), context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	var mu sync.Mutex
	stopped := false
	deadline := time.Now().Add(timeout)
	var timer *time.Timer
	expire := func() {
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		// A timer callback may already be queued when progress renews the
		// lease. Recheck the mutex-protected deadline instead of allowing that
		// stale callback to cancel a healthy turn.
		if remaining := time.Until(deadline); remaining > 0 {
			timer.Reset(remaining)
			mu.Unlock()
			return
		}
		stopped = true
		mu.Unlock()
		if parent.Err() != nil {
			cancel(context.Cause(parent))
			return
		}
		cancel(ErrProviderStalled)
	}
	timer = time.AfterFunc(timeout, expire)
	stop := func() {
		mu.Lock()
		stopped = true
		timer.Stop()
		mu.Unlock()
		cancel(context.Canceled)
	}
	progress := func() {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			deadline = time.Now().Add(timeout)
			timer.Reset(timeout)
		}
	}
	return ctx, progress, stop
}

// progressWriter renews the stall lease on observed output and enforces the
// output ceiling AS IT STREAMS.
//
// The ceiling cannot wait for Run to return. Every write renews the lease, so a
// process that emits continuously never stalls and runs until the total turn
// deadline — with that deadline now independently configurable up to 24h, an
// unbounded sink would let a noisy provider consume arbitrary memory and OOM the
// supervisor long before the orchestrator's post-Run ValidateEvidenceBounds
// could reject it. Bounding here converts that into a deterministic, retryable
// turn failure, and the caller cancels the process as soon as Write reports it.
type progressWriter struct {
	io.Writer
	progress func()

	// limit is the maximum number of bytes this writer will accept. Zero means
	// unlimited, for callers that have their own bounded sink.
	limit int
	// n is the running total of bytes accepted.
	n *int
}

func (w progressWriter) Write(p []byte) (int, error) {
	if w.limit > 0 && w.n != nil {
		remaining := w.limit - *w.n
		if remaining <= 0 {
			return 0, ErrProviderOutputTooLarge
		}
		if len(p) > remaining {
			// Accept what fits so the error names a real ceiling crossing
			// rather than discarding a partial line silently, then refuse.
			n, err := w.Writer.Write(p[:remaining])
			*w.n += n
			if err != nil {
				return n, err
			}
			return n, ErrProviderOutputTooLarge
		}
	}
	n, err := w.Writer.Write(p)
	if n > 0 {
		if w.n != nil {
			*w.n += n
		}
		w.progress()
	}
	return n, err
}

type progressReader struct {
	io.Reader
	progress func()
}

func (r progressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.progress()
	}
	return n, err
}
