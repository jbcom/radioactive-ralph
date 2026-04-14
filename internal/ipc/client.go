package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Client wraps a single Unix-socket connection to a Ralph supervisor.
// Clients are short-lived: construct, send one command, read reply, close.
// For streaming commands (attach), the client instance stays open until
// the server closes the stream.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
}

// Dial connects to the supervisor at socketPath with the given timeout.
// Typical usage:
//
//	c, err := ipc.Dial(socketPath, 3*time.Second)
//	if err != nil { ... }
//	defer c.Close()
//	status, err := c.Status(ctx)
func Dial(socketPath string, timeout time.Duration) (*Client, error) {
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial %s: %w", socketPath, err)
	}
	return &Client{
		conn:   conn,
		reader: bufio.NewReader(conn),
	}, nil
}

// Close terminates the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Status issues a status request and returns the parsed StatusReply.
func (c *Client) Status(ctx context.Context) (StatusReply, error) {
	var zero StatusReply
	if err := c.send(ctx, Request{Cmd: CmdStatus}); err != nil {
		return zero, err
	}
	resp, err := c.readResponse()
	if err != nil {
		return zero, err
	}
	if !resp.Ok {
		return zero, errors.New(resp.Error)
	}
	var reply StatusReply
	if err := json.Unmarshal(resp.Data, &reply); err != nil {
		return zero, fmt.Errorf("ipc: decode status reply: %w", err)
	}
	return reply, nil
}

// Enqueue pushes a task. Returns the resulting task ID (possibly a
// dedup hit from FTS) and whether the task was freshly inserted.
func (c *Client) Enqueue(ctx context.Context, args EnqueueArgs) (EnqueueReply, error) {
	var zero EnqueueReply
	body, err := json.Marshal(args)
	if err != nil {
		return zero, fmt.Errorf("ipc: marshal enqueue args: %w", err)
	}
	if err := c.send(ctx, Request{Cmd: CmdEnqueue, Args: body}); err != nil {
		return zero, err
	}
	resp, err := c.readResponse()
	if err != nil {
		return zero, err
	}
	if !resp.Ok {
		return zero, errors.New(resp.Error)
	}
	var reply EnqueueReply
	if err := json.Unmarshal(resp.Data, &reply); err != nil {
		return zero, fmt.Errorf("ipc: decode enqueue reply: %w", err)
	}
	return reply, nil
}

// Stop issues a stop request. The server closes the socket after
// replying; expect the returned error to be ErrClosed on the next call.
func (c *Client) Stop(ctx context.Context, args StopArgs) error {
	body, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("ipc: marshal stop args: %w", err)
	}
	if err := c.send(ctx, Request{Cmd: CmdStop, Args: body}); err != nil {
		return err
	}
	resp, err := c.readResponse()
	if err != nil {
		return err
	}
	if !resp.Ok {
		return errors.New(resp.Error)
	}
	return nil
}

// ReloadConfig asks the supervisor to re-read config.toml.
func (c *Client) ReloadConfig(ctx context.Context) error {
	if err := c.send(ctx, Request{Cmd: CmdReloadConfig}); err != nil {
		return err
	}
	resp, err := c.readResponse()
	if err != nil {
		return err
	}
	if !resp.Ok {
		return errors.New(resp.Error)
	}
	return nil
}

// Attach issues an attach request and streams event frames through fn
// until the supervisor closes the stream or ctx is cancelled. The
// returned error is nil for a clean end-of-stream.
func (c *Client) Attach(ctx context.Context, fn func(json.RawMessage) error) error {
	if err := c.send(ctx, Request{Cmd: CmdAttach}); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrClosed
			}
			return fmt.Errorf("ipc: read attach frame: %w", err)
		}
		// A frame is either a StreamEvent ({"event": ...}) or the
		// final Response ({"ok": true/false, "error": "..."}).
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(line, &probe); err != nil {
			return fmt.Errorf("ipc: decode attach frame: %w", err)
		}
		if ev, ok := probe["event"]; ok {
			if err := fn(ev); err != nil {
				return err
			}
			continue
		}
		// final Response
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			return fmt.Errorf("ipc: decode final response: %w", err)
		}
		if !resp.Ok {
			return errors.New(resp.Error)
		}
		return nil
	}
}

func (c *Client) send(ctx context.Context, req Request) error {
	frame, err := encodeJSONLine(req)
	if err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
	}
	if _, err := c.conn.Write(frame); err != nil {
		return fmt.Errorf("ipc: write request: %w", err)
	}
	return nil
}

func (c *Client) readResponse() (Response, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return Response{}, ErrClosed
		}
		return Response{}, fmt.Errorf("ipc: read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("ipc: decode response: %w", err)
	}
	return resp, nil
}
