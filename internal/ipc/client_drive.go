package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// CodedError wraps a !Ok Response, exposing both the human message and the
// stable machine-readable error class (Code* consts) so a caller (the GUI) can
// branch on the failure kind — e.g. treat CodeNotFound as benign. It satisfies
// the Coded interface.
type CodedError struct {
	Class   string
	Message string
}

func (e *CodedError) Error() string {
	if e.Class != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Class)
	}
	return e.Message
}

// Code returns the error class, satisfying Coded.
func (e *CodedError) Code() string { return e.Class }

// IsCode reports whether err carries the given error class. It matches any
// error implementing the Coded interface (Code() string) — both the client's
// *CodedError (decoded from a wire Response.Code) and a handler-side coded
// error returned by a direct in-process call.
func IsCode(err error, code string) bool {
	var c Coded
	return errors.As(err, &c) && c.Code() == code
}

// driveCall sends a drive command with JSON-encoded args and decodes the reply
// into out (out may be nil for OK-only commands). A !Ok response becomes a
// *CodedError carrying the response Code.
func (c *Client) driveCall(ctx context.Context, cmd string, args any, out any) error {
	body, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("ipc: marshal %s args: %w", cmd, err)
	}
	if err := c.send(ctx, Request{Cmd: cmd, Args: body, ProtoVersion: ProtoVersion}); err != nil {
		return err
	}
	resp, err := c.readResponse(ctx)
	if err != nil {
		return err
	}
	if !resp.Ok {
		return &CodedError{Class: resp.Code, Message: resp.Error}
	}
	if out != nil && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("ipc: decode %s reply: %w", cmd, err)
		}
	}
	return nil
}

// PlanImport imports a markdown plan and activates it, returning the created
// plan's id/slug/title.
func (c *Client) PlanImport(ctx context.Context, args PlanImportArgs) (PlanImportReply, error) {
	var reply PlanImportReply
	err := c.driveCall(ctx, CmdPlanImport, args, &reply)
	return reply, err
}

// PlanSetStatus changes a plan's lifecycle status (paused|active|abandoned).
func (c *Client) PlanSetStatus(ctx context.Context, args PlanSetStatusArgs) (PlanSetStatusReply, error) {
	var reply PlanSetStatusReply
	err := c.driveCall(ctx, CmdPlanSetStatus, args, &reply)
	return reply, err
}

// TaskApprove clears the approval gate on a ready_pending_approval task.
func (c *Client) TaskApprove(ctx context.Context, args TaskApproveArgs) error {
	return c.driveCall(ctx, CmdTaskApprove, args, nil)
}

// WorkerKill kills a running worker via kill-and-reclaim.
func (c *Client) WorkerKill(ctx context.Context, args WorkerKillArgs) error {
	return c.driveCall(ctx, CmdWorkerKill, args, nil)
}

// NegotiatedVersion returns the supervisor's supported wire protocol version
// (from StatusReply). 0 means a pre-versioned v1 supervisor.
func (c *Client) NegotiatedVersion(ctx context.Context) (int, error) {
	st, err := c.Status(ctx)
	if err != nil {
		return 0, err
	}
	return st.ProtoVersion, nil
}
