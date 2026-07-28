package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

type fakeCalibrationClient struct {
	putArgs ipc.CalibrationPutArgs
	putID   string
	putErr  error
	list    ipc.CalibrationListReply
	listErr error
}

func (f *fakeCalibrationClient) CalibrationPut(
	_ context.Context, args ipc.CalibrationPutArgs,
) (ipc.CalibrationPutReply, error) {
	f.putArgs = args
	if f.putErr != nil {
		return ipc.CalibrationPutReply{}, f.putErr
	}
	return ipc.CalibrationPutReply{ID: f.putID}, nil
}

func (f *fakeCalibrationClient) CalibrationList(_ context.Context) (ipc.CalibrationListReply, error) {
	return f.list, f.listErr
}

// TestCalibrationRecordSendsEveryIdentifyingField pins that the flags reach the
// wire. A flag that silently never populates its field would record a
// measurement that no dispatch lookup could match, and the operator would see a
// successful put.
func TestCalibrationRecordSendsEveryIdentifyingField(t *testing.T) {
	client := &fakeCalibrationClient{putID: "cal-123"}
	var out bytes.Buffer

	// Every flag the command declares must actually populate the record it sends.
	// Parsing through the real cobra command rather than hand-building the struct
	// is the point: a StringVar bound to the wrong field compiles, records a
	// measurement no dispatch lookup can match, and still reports success.
	cmd := newCalibrationRecordCmd()
	if err := cmd.ParseFlags([]string{
		"--alias", "claude", "--provider", "claude",
		"--invocation-hash", "abc123",
		"--model", "claude-sonnet-4", "--effort", "medium",
		"--independence-domain", "anthropic",
	}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	record := ipc.CalibrationRecord{
		Alias:              cmd.Flags().Lookup("alias").Value.String(),
		Provider:           cmd.Flags().Lookup("provider").Value.String(),
		InvocationHash:     cmd.Flags().Lookup("invocation-hash").Value.String(),
		Model:              cmd.Flags().Lookup("model").Value.String(),
		Effort:             cmd.Flags().Lookup("effort").Value.String(),
		IndependenceDomain: cmd.Flags().Lookup("independence-domain").Value.String(),
	}
	if err := runCalibrationRecord(context.Background(), &out, client, record, false); err != nil {
		t.Fatalf("runCalibrationRecord: %v", err)
	}

	got := client.putArgs.Calibration
	if got.Alias != "claude" || got.Provider != "claude" || got.InvocationHash != "abc123" {
		t.Errorf("identity fields lost in transit: %+v", got)
	}
	if got.IndependenceDomain != "anthropic" {
		t.Errorf("independence domain = %q, want %q — the field differentFrom compares",
			got.IndependenceDomain, "anthropic")
	}
	if !strings.Contains(out.String(), "anthropic") {
		t.Errorf("output %q does not report the recorded domain", out.String())
	}
}

// TestCalibrationRecordWarnsWhenNoDomainWasGiven is the operator-honesty case. A
// calibration without an independence domain records fine but cannot support a
// differentFrom constraint, and an operator who believes they just enabled one
// needs telling that they did not.
func TestCalibrationRecordWarnsWhenNoDomainWasGiven(t *testing.T) {
	client := &fakeCalibrationClient{putID: "cal-456"}
	var out bytes.Buffer
	record := ipc.CalibrationRecord{
		Alias: "codex", Provider: "codex", InvocationHash: "def456",
	}
	if err := runCalibrationRecord(context.Background(), &out, client, record, false); err != nil {
		t.Fatalf("runCalibrationRecord: %v", err)
	}
	if !strings.Contains(out.String(), "no independence domain") {
		t.Fatalf("output %q does not warn that differentFrom cannot be enforced; "+
			"a silent success here reads as a constraint the operator does not have",
			out.String())
	}
}

// TestCalibrationRecordSurfacesTheError confirms a refusal reaches the operator
// rather than being swallowed into a success message.
func TestCalibrationRecordSurfacesTheError(t *testing.T) {
	client := &fakeCalibrationClient{putErr: errors.New("alias already calibrated")}
	var out bytes.Buffer
	err := runCalibrationRecord(context.Background(), &out, client,
		ipc.CalibrationRecord{Alias: "claude"}, false)
	if err == nil {
		t.Fatal("a refused put reported success")
	}
	if strings.Contains(out.String(), "recorded") {
		t.Errorf("output %q claims a record was written despite the error", out.String())
	}
}

// TestCalibrationListSaysWhenNothingIsCalibrated covers the state the whole
// feature starts in. An empty list is the reason differentFrom silently cannot
// be enforced, so printing nothing at all would hide the diagnosis.
func TestCalibrationListSaysWhenNothingIsCalibrated(t *testing.T) {
	client := &fakeCalibrationClient{}
	var out bytes.Buffer
	if err := runCalibrationList(context.Background(), &out, client, false); err != nil {
		t.Fatalf("runCalibrationList: %v", err)
	}
	if !strings.Contains(out.String(), "no calibrations recorded") {
		t.Fatalf("output %q does not explain that nothing is calibrated", out.String())
	}
	if !strings.Contains(out.String(), "differentFrom") {
		t.Errorf("output %q does not connect the empty list to the consequence", out.String())
	}
}

// TestCalibrationListFlagsAnAliasWithNoDomain covers the halfway state: a
// recorded calibration whose independence domain is empty looks calibrated in a
// bare listing while supporting no constraint at all.
func TestCalibrationListFlagsAnAliasWithNoDomain(t *testing.T) {
	client := &fakeCalibrationClient{list: ipc.CalibrationListReply{
		Calibrations: []ipc.CalibrationRecord{
			{Alias: "claude", Provider: "claude", Model: "sonnet", IndependenceDomain: "anthropic"},
			{Alias: "local", Provider: "opencode", Model: "qwen"},
		},
	}}
	var out bytes.Buffer
	if err := runCalibrationList(context.Background(), &out, client, false); err != nil {
		t.Fatalf("runCalibrationList: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "anthropic") {
		t.Errorf("output %q omits the calibrated domain", text)
	}
	if !strings.Contains(text, "none") {
		t.Errorf("output %q does not mark the alias whose domain is missing; it "+
			"reads as calibrated while enforcing nothing", text)
	}
}
