package agent

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type emptyReadCounter struct {
	calls int
}

func (r *emptyReadCounter) Read([]byte) (int, error) {
	r.calls++
	return 0, nil
}

type resetEmptyReader struct {
	calls int
}

func (r *resetEmptyReader) Read(p []byte) (int, error) {
	r.calls++
	switch r.calls {
	case maxConsecutiveEmptyReads:
		p[0] = 'x'
		return 1, nil
	case 2 * maxConsecutiveEmptyReads:
		p[0] = '\n'
		return 1, nil
	default:
		return 0, nil
	}
}

type cancelingEmptyReader struct {
	calls  int
	cancel func()
}

func (r *cancelingEmptyReader) Read([]byte) (int, error) {
	r.calls++
	if r.calls == 7 {
		r.cancel()
	}
	return 0, nil
}

func TestOutputReaderRejectsExactConsecutiveEmptyReadThreshold(t *testing.T) {
	source := &emptyReadCounter{}
	line, discarded, err := newOutputLineReader(source, nil).nextLine(
		64,
		RejectOversizeOutput,
	)
	if !errors.Is(err, ErrOutputRead) || line != nil || discarded {
		t.Fatalf("empty-read result = (%q, %v, %v), want static read failure", line, discarded, err)
	}
	if source.calls != maxConsecutiveEmptyReads {
		t.Fatalf("Read calls = %d, want bounded threshold %d", source.calls, maxConsecutiveEmptyReads)
	}
}

func TestOutputReaderEmptyReadCounterResetsOnProgress(t *testing.T) {
	source := &resetEmptyReader{}
	line, discarded, err := newOutputLineReader(source, nil).nextLine(
		64,
		RejectOversizeOutput,
	)
	if err != nil || discarded || string(line) != "x\n" {
		t.Fatalf("reset result = (%q, %v, %v), want x newline", line, discarded, err)
	}
	if source.calls != 2*maxConsecutiveEmptyReads {
		t.Fatalf("Read calls = %d, want %d across reset", source.calls, 2*maxConsecutiveEmptyReads)
	}
}

func TestOutputReaderCancellationStopsEmptyReadsBeforeThreshold(t *testing.T) {
	stop := make(chan struct{})
	source := &cancelingEmptyReader{cancel: func() { close(stop) }}
	reader := newOutputLineReader(source, nil)
	reader.stop = stop

	line, discarded, err := reader.nextLine(64, RejectOversizeOutput)
	if !errors.Is(err, errOutputStopped) || line != nil || discarded {
		t.Fatalf("cancel result = (%q, %v, %v), want content-free stop", line, discarded, err)
	}
	if source.calls != 7 {
		t.Fatalf("Read calls after cancellation = %d, want 7", source.calls)
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("cancellation was misreported as natural EOF")
	}
}

func TestOutputReaderCumulativeObservedByteCeiling(t *testing.T) {
	t.Run("exact partial line succeeds", func(t *testing.T) {
		const limit = 8
		reader := newOutputLineReader(strings.NewReader("12345678"), nil, limit)
		line, discarded, err := reader.nextLine(limit, RejectOversizeOutput)
		if err != nil || discarded || string(line) != "12345678\n" {
			t.Fatalf("result = (%q, %v, %v), want exact partial line", line, discarded, err)
		}
		if reader.observedBytes != limit {
			t.Fatalf("observed bytes = %d, want exact limit %d", reader.observedBytes, limit)
		}
	})

	t.Run("partial line limit plus one fails statically", func(t *testing.T) {
		const limit = 8
		reader := newOutputLineReader(strings.NewReader("123456789"), nil, limit)
		line, discarded, err := reader.nextLine(limit+1, RejectOversizeOutput)
		if err != ErrObservedOutputTooLarge || line != nil || discarded {
			t.Fatalf("result = (%q, %v, %v), want static observed-output failure", line, discarded, err)
		}
		if reader.observedBytes != limit+1 {
			t.Fatalf("observed bytes = %d, want %d", reader.observedBytes, limit+1)
		}
	})

	t.Run("discarded oversized line still crosses raw ceiling", func(t *testing.T) {
		const limit = outputReadBufferBytes + 1
		source := strings.NewReader(strings.Repeat("x", limit+1))
		reader := newOutputLineReader(source, nil, limit)
		line, _, err := reader.nextLine(4<<10, DiscardOversizeOutput)
		if err != ErrObservedOutputTooLarge || line != nil {
			t.Fatalf("result = (%q, %v), want static observed-output failure", line, err)
		}
		if reader.observedBytes != limit+1 {
			t.Fatalf("observed bytes = %d, want %d", reader.observedBytes, limit+1)
		}
	})

	t.Run("discarded line exposes only bounded prefix", func(t *testing.T) {
		payload := strings.Repeat("prefix-", 2<<10) + "\n"
		reader := newOutputLineReader(strings.NewReader(payload), nil, 0)
		line, discarded, err := reader.nextLine(4<<10, DiscardOversizeOutput)
		if err != nil || line != nil || !discarded {
			t.Fatalf("result = (%q, %v, %v), want discarded line", line, discarded, err)
		}
		prefix := reader.takeDiscardedPrefix()
		if len(prefix) != maxDiscardedOutputPrefixBytes {
			t.Fatalf("discarded prefix = %d bytes, want %d", len(prefix), maxDiscardedOutputPrefixBytes)
		}
		if string(prefix) != payload[:maxDiscardedOutputPrefixBytes] {
			t.Fatal("discarded prefix did not preserve the record start")
		}
		if extra := reader.takeDiscardedPrefix(); extra != nil {
			t.Fatalf("discarded prefix was retained after transfer: %q", extra)
		}
	})

	t.Run("single first segment exceeds tiny retention but keeps classifier prefix", func(t *testing.T) {
		payload := strings.Repeat("z", maxDiscardedOutputPrefixBytes+512) + "\n"
		reader := newOutputLineReader(strings.NewReader(payload), nil, 0)
		line, discarded, err := reader.nextLine(16, DiscardOversizeOutput)
		if err != nil || line != nil || !discarded {
			t.Fatalf("result = (%q, %v, %v), want discarded line", line, discarded, err)
		}
		prefix := reader.takeDiscardedPrefix()
		if len(prefix) != maxDiscardedOutputPrefixBytes {
			t.Fatalf("discarded prefix = %d bytes, want %d", len(prefix), maxDiscardedOutputPrefixBytes)
		}
		if string(prefix) != payload[:maxDiscardedOutputPrefixBytes] {
			t.Fatal("tiny-retention discard lost bytes from the first read segment")
		}
	})

	t.Run("zero is unlimited", func(t *testing.T) {
		const payloadBytes = 2*outputReadBufferBytes + 17
		reader := newOutputLineReader(
			strings.NewReader(strings.Repeat("x", payloadBytes)),
			nil,
			0,
		)
		line, discarded, err := reader.nextLine(payloadBytes, RejectOversizeOutput)
		if err != nil || discarded || len(line) != payloadBytes+1 {
			t.Fatalf(
				"result = (%d bytes, %v, %v), want unlimited payload plus newline",
				len(line),
				discarded,
				err,
			)
		}
	})
}
