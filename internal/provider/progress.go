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

type progressWriter struct {
	io.Writer
	progress func()
}

func (w progressWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n > 0 {
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
