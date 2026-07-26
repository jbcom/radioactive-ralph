package agent

import (
	"bytes"
	"errors"
	"io"
	"time"
)

const (
	outputReadBufferBytes = 64 << 10
	outputRetentionSlots  = 4
	// Three discarded prefixes can be live concurrently: the provider callback
	// owns one, Watch owns one awaiting supervisor admission, and readLoop owns
	// the next before its unbuffered Watch handoff. Keep this symmetric with all
	// aggregate/inverse retention arithmetic.
	discardedPrefixRetentionSlots = 3
	// A discarded record exposes only this bounded prefix to a provider-specific
	// classifier. The three pipeline-owner allocations are independently
	// reserved in the aggregate retention budget and are never rendered,
	// prompt-matched, or interpolated into an error.
	maxDiscardedOutputPrefixBytes = 4 << 10
	// This matches the standard library's bounded no-progress convention:
	// Readers that return (0, nil) repeatedly are considered broken after
	// 100 consecutive calls rather than being allowed to busy-spin forever.
	maxConsecutiveEmptyReads = 100
)

var errOutputStopped = errors.New("agent: output reader stopped")

type outputLineReader struct {
	reader           io.Reader
	onRead           func()
	stop             <-chan struct{}
	buffer           []byte
	start            int
	end              int
	pendingErr       error
	emptyReads       int
	maxObservedBytes int64
	observedBytes    uint64
	discardedPrefix  []byte
}

func newOutputLineReader(
	reader io.Reader,
	onRead func(),
	maxObservedBytes ...int64,
) *outputLineReader {
	var maximum int64
	if len(maxObservedBytes) > 0 {
		maximum = maxObservedBytes[0]
	}
	return &outputLineReader{
		reader:           reader,
		onRead:           onRead,
		buffer:           make([]byte, outputReadBufferBytes),
		maxObservedBytes: maximum,
	}
}

func (a *Agent) readLoop() {
	// PTY EOF means output ended, not execution ended. Natural observation or
	// explicit termination closes terminal; only then may Output and Done close.
	defer func() {
		<-a.terminal
		a.closePTY()
		close(a.discarded)
		close(a.out)
		close(a.done)
	}()

	reader := newOutputLineReader(
		a.pty,
		a.noteOutputActivity,
		a.opts.MaxObservedOutputBytes,
	)
	reader.stop = a.killed
	abandoning := false
	for {
		if !abandoning {
			select {
			case <-a.abandonOutput:
				abandoning = true
			default:
			}
		}
		line, discarded, err := reader.nextLine(
			a.maxRetainedLineBytes,
			a.opts.OversizeOutputPolicy,
		)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			if errors.Is(err, errOutputStopped) {
				return
			}
			select {
			case <-a.killed:
				return
			default:
			}
			a.outputMu.Lock()
			a.outputErr = err
			a.outputMu.Unlock()
			// A reader-contract failure is terminal. Explicit termination owns
			// reaping even if the natural observer is unhealthy.
			if cleanupErr := a.terminateProcess(); cleanupErr != nil {
				a.outputMu.Lock()
				a.outputErr = errors.Join(a.outputErr, cleanupErr)
				a.outputMu.Unlock()
			}
			return
		}
		if discarded {
			prefix := reader.takeDiscardedPrefix()
			if abandoning {
				continue
			}
			select {
			case a.discarded <- prefix:
			case <-a.abandonOutput:
				abandoning = true
			case <-a.killed:
				return
			}
			continue
		}
		if abandoning {
			continue
		}
		// Both transport stages are unbuffered, so neither can accumulate a
		// queue. The aggregate budget reserves the callback-owned line, Watch's
		// admission line, this reader's current line, growth overlap, and the
		// analogous three-owner discarded-prefix pipeline above.
		select {
		case a.out <- line:
		case <-a.abandonOutput:
			abandoning = true
		case <-a.killed:
			return
		}
	}
}

func (r *outputLineReader) nextLine(
	retainedLineBytes int,
	policy OversizeOutputPolicy,
) ([]byte, bool, error) {
	retainCap := retainedLineBytes + 1 // possible CR before a pending LF
	var line []byte
	discarded := false
	var discardedPrefix []byte

	for {
		if r.start == r.end {
			if r.pendingErr != nil {
				err := r.pendingErr
				r.pendingErr = nil
				r.emptyReads = 0
				if errors.Is(err, io.EOF) || isPTYEOF(err) {
					if len(line) == 0 && !discarded {
						return nil, false, io.EOF
					}
					if discarded {
						r.discardedPrefix = discardedPrefix
						return nil, true, nil
					}
					if len(line) > retainedLineBytes {
						return oversizeLine(policy)
					}
					line, _ = appendWithinCap(line, []byte{'\n'}, retainCap)
					return line, false, nil
				}
				return nil, false, ErrOutputRead
			}
			if r.stopped() {
				return nil, false, errOutputStopped
			}
			if err := r.readMore(); err != nil {
				return nil, false, err
			}
			if r.start == r.end {
				continue
			}
		}

		pending := r.buffer[r.start:r.end]
		newlineAt := bytes.IndexByte(pending, '\n')
		segment := pending
		if newlineAt >= 0 {
			segment = pending[:newlineAt]
			r.start += newlineAt + 1
		} else {
			r.start = r.end
		}

		if !discarded {
			var ok bool
			lineBytesBeforeAppend := len(line)
			line, ok = appendWithinCap(line, segment, retainCap)
			if !ok || (len(line) > retainedLineBytes &&
				(len(line) != retainedLineBytes+1 || line[len(line)-1] != '\r')) {
				if policy == RejectOversizeOutput {
					return nil, false, ErrOutputLineTooLong
				}
				discarded = true
				discardedPrefix = makeDiscardedOutputPrefix(
					line,
					lineBytesBeforeAppend,
					segment,
					ok,
				)
				line = nil // release retained content while the rest is drained
			}
		}

		if newlineAt >= 0 {
			if discarded {
				r.discardedPrefix = discardedPrefix
				return nil, true, nil
			}
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			line, _ = appendWithinCap(line, []byte{'\n'}, retainCap)
			return line, false, nil
		}
	}
}

func makeDiscardedOutputPrefix(
	line []byte,
	lineBytesBeforeAppend int,
	segment []byte,
	appendComplete bool,
) []byte {
	if appendComplete {
		prefixBytes := min(len(line), maxDiscardedOutputPrefixBytes)
		return append(make([]byte, 0, prefixBytes), line[:prefixBytes]...)
	}

	prefixBytes := min(
		lineBytesBeforeAppend+len(segment),
		maxDiscardedOutputPrefixBytes,
	)
	prefix := make([]byte, 0, prefixBytes)
	fromLine := min(lineBytesBeforeAppend, prefixBytes)
	prefix = append(prefix, line[:fromLine]...)
	fromSegment := prefixBytes - fromLine
	return append(prefix, segment[:fromSegment]...)
}

func (r *outputLineReader) readMore() error {
	readBuffer := r.buffer
	remainingObserved := uint64(0)
	if r.maxObservedBytes > 0 {
		remainingObserved = uint64(r.maxObservedBytes) - r.observedBytes
		if remainingObserved < uint64(len(readBuffer)) {
			// The comparison above proves this conversion is in [0, len(buffer)).
			remainingReadBytes := int(remainingObserved) //nolint:gosec
			// Read at most one byte beyond the remaining allowance. That byte
			// proves the ceiling was crossed without a full-buffer work overshoot.
			readBuffer = readBuffer[:remainingReadBytes+1]
		}
	}
	n, err := r.reader.Read(readBuffer)
	if n < 0 || n > len(readBuffer) {
		return ErrOutputRead
	}
	if n > 0 {
		r.emptyReads = 0
		if r.onRead != nil {
			r.onRead()
		}
		if r.maxObservedBytes > 0 {
			r.observedBytes += uint64(n)
			if uint64(n) > remainingObserved {
				return ErrObservedOutputTooLarge
			}
		}
		r.start = 0
		r.end = n
		r.pendingErr = err
		return nil
	}
	if err != nil {
		r.emptyReads = 0
		r.pendingErr = err
		return nil
	}
	r.emptyReads++
	if r.emptyReads >= maxConsecutiveEmptyReads {
		return ErrOutputRead
	}
	if r.stopped() {
		return errOutputStopped
	}
	return nil
}

func (r *outputLineReader) takeDiscardedPrefix() []byte {
	prefix := r.discardedPrefix
	r.discardedPrefix = nil
	return prefix
}

func (r *outputLineReader) stopped() bool {
	if r.stop == nil {
		return false
	}
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
}

func appendWithinCap(dst, src []byte, maximum int) ([]byte, bool) {
	complete := true
	if len(src) > maximum-len(dst) {
		src = src[:maximum-len(dst)]
		complete = false
	}
	needed := len(dst) + len(src)
	if needed > cap(dst) {
		nextCap := cap(dst) * 2
		if nextCap < 4<<10 {
			nextCap = 4 << 10
		}
		if nextCap < needed {
			nextCap = needed
		}
		if nextCap > maximum {
			nextCap = maximum
		}
		next := make([]byte, len(dst), nextCap)
		copy(next, dst)
		dst = next
	}
	return append(dst, src...), complete
}

func oversizeLine(policy OversizeOutputPolicy) ([]byte, bool, error) {
	if policy == DiscardOversizeOutput {
		return nil, true, nil
	}
	return nil, false, ErrOutputLineTooLong
}

func (a *Agent) noteOutputActivity() {
	observedAt := time.Now()
	select {
	case a.activity <- observedAt:
		return
	default:
	}
	// Coalesce to the NEWEST read time, not the oldest unread pulse. The sole
	// writer is readLoop; a racing Watch receive can only make room for the
	// fresh timestamp, never grow this channel beyond one value.
	select {
	case <-a.activity:
	default:
	}
	select {
	case a.activity <- observedAt:
	default:
	}
}

// Activity is a content-free liveness stream. Its timestamp records when the
// underlying pty read returned bytes, including while a partial or discarded
// line is still being drained. The timestamp prevents downstream backpressure
// from turning an old queued pulse into a fresh stall-timeout lease.
func (a *Agent) Activity() <-chan time.Time { return a.activity }

// DiscardedOutput is an unbuffered stream of bounded prefixes from records
// drained under DiscardOversizeOutput. Prefixes are intended only for
// provider-specific top-level framing classification. They are never pane
// output, never prompt-matched, and close before Output.
func (a *Agent) DiscardedOutput() <-chan []byte { return a.discarded }

// OutputErr returns a bounded-reader or process-tree cleanup failure, if one
// occurred. It never includes terminal contents. Callers may read it once
// Output closes; the mutex also makes earlier diagnostic reads safe.
func (a *Agent) OutputErr() error {
	a.outputMu.Lock()
	defer a.outputMu.Unlock()
	return a.outputErr
}
