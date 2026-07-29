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

// driveCall sends one v2 drive command. It deliberately uses the command's
// minimum protocol rather than this binary's maximum version so a rolling v3
// client can still drive an otherwise-compatible v2 supervisor.
func (c *Client) driveCall(ctx context.Context, cmd string, args any, out any) error {
	return c.versionedCall(ctx, DriveProtoVersion, cmd, args, out)
}

// projectEnsureProtoVersion picks the floor for one ProjectEnsure request:
// ResolveOnly changes the command's semantics and needs a supervisor that
// understands it, while a plain request stays on the drive version so rolling
// upgrades keep working. See ResolveOnlyProtoVersion.
func projectEnsureProtoVersion(args ProjectEnsureArgs) int {
	if args.ResolveOnly {
		return ResolveOnlyProtoVersion
	}
	return DriveProtoVersion
}

// versionedCall sends a command-scoped protocol version with JSON-encoded args
// and decodes the reply into out (out may be nil for OK-only commands). A !Ok
// response becomes a *CodedError carrying the response Code.
func (c *Client) versionedCall(
	ctx context.Context,
	protoVersion int,
	cmd string,
	args any,
	out any,
) error {
	body, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("ipc: marshal %s args: %w", cmd, err)
	}
	if err := c.send(ctx, Request{
		Cmd:          cmd,
		Args:         body,
		ProtoVersion: protoVersion,
	}); err != nil {
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

// PlanDelete removes a plan and everything hanging off it. Irreversible: the
// plan's tasks, deps, and events go with it.
func (c *Client) PlanDelete(ctx context.Context, args PlanDeleteArgs) (PlanDeleteReply, error) {
	var reply PlanDeleteReply
	err := c.driveCall(ctx, CmdPlanDelete, args, &reply)
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

// ProjectEnsure resolves the caller's directory to a project, creating it when
// no fingerprint matches. The client computes its own fingerprints from its
// working directory; the supervisor owns the store write.
func (c *Client) ProjectEnsure(
	ctx context.Context,
	args ProjectEnsureArgs,
) (*ProjectEnsureReply, error) {
	var reply ProjectEnsureReply
	if err := c.versionedCall(
		ctx, projectEnsureProtoVersion(args), CmdProjectEnsure, args, &reply,
	); err != nil {
		return nil, err
	}
	return &reply, nil
}

// ProjectConfigGet reads a project's stored config values through the
// supervisor, so a client never opens the store to resolve its config layers.
func (c *Client) ProjectConfigGet(ctx context.Context, args ProjectConfigGetArgs) (ProjectConfigGetReply, error) {
	var reply ProjectConfigGetReply
	if err := c.driveCall(ctx, CmdProjectConfigGet, args, &reply); err != nil {
		return ProjectConfigGetReply{}, err
	}
	return reply, nil
}

// ProjectConfigApply upserts and deletes project config keys in one call.
func (c *Client) ProjectConfigApply(ctx context.Context, args ProjectConfigApplyArgs) error {
	return c.driveCall(ctx, CmdProjectConfigApply, args, nil)
}

// CalibrationPut records one provider calibration and returns the
// content-addressed id the store derived.
//
// On the DRIVE version rather than the query one: recording a measurement is a
// write, and the supervisor stays the single writer of record.
func (c *Client) CalibrationPut(ctx context.Context, args CalibrationPutArgs) (CalibrationPutReply, error) {
	var reply CalibrationPutReply
	if err := c.driveCall(ctx, CmdCalibrationPut, args, &reply); err != nil {
		return CalibrationPutReply{}, err
	}
	return reply, nil
}

// CalibrationList enumerates the recorded calibrations, one per alias, so an
// operator can see which aliases have a measured independence domain — the
// difference between a differentFrom constraint that can be enforced and one
// that silently cannot.
func (c *Client) CalibrationList(ctx context.Context) (CalibrationListReply, error) {
	var reply CalibrationListReply
	if err := c.driveCall(ctx, CmdCalibrationList, struct{}{}, &reply); err != nil {
		return CalibrationListReply{}, err
	}
	return reply, nil
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
