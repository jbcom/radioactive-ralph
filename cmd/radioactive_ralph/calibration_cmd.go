package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// calibrationClient is the slice of the IPC client these commands need, so the
// tests can drive them without a live supervisor.
type calibrationClient interface {
	CalibrationPut(context.Context, ipc.CalibrationPutArgs) (ipc.CalibrationPutReply, error)
	CalibrationList(context.Context) (ipc.CalibrationListReply, error)
}

// newCalibrationCmd is the OPERATOR-FACING path for the independence domain.
//
// Without it the supervisor could accept a calibration but nothing shipped could
// send one, so the table stayed empty, every task recorded an empty independence
// domain, and a differentFrom constraint compared "" against "" and permitted
// everything. A producer with no way to invoke it is not a producer.
func newCalibrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calibration",
		Short: "Record and inspect provider calibrations",
		Long: "A calibration is a MEASUREMENT of one provider command line, " +
			"including the independence domain a plan's differentFrom constraint " +
			"is evaluated against. Recording goes through the supervisor, which " +
			"stays the single writer of record.",
	}
	cmd.AddCommand(newCalibrationRecordCmd())
	cmd.AddCommand(newCalibrationListCmd())
	return cmd
}

func newCalibrationRecordCmd() *cobra.Command {
	var (
		record ipc.CalibrationRecord
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record one provider calibration",
		Long: "Record a measurement of one provider command line. " +
			"--invocation-hash identifies the exact command line measured: " +
			"dispatch reuses a calibration's independence domain ONLY when that " +
			"hash matches the invocation it resolves, so a hash from a different " +
			"binding config, model, or effort is deliberately ignored rather than " +
			"applied as stale evidence. " +
			"Re-recording the same measurement is idempotent; recording a " +
			"DIFFERENT one under a live alias is refused rather than overwritten, " +
			"because replacing it would retroactively change what already- " +
			"dispatched tasks are believed to have run on.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := calibrationStateRoot()
			if err != nil {
				return err
			}
			client, err := supervisor.Find(root)
			if err != nil {
				return errNoSupervisorListening
			}
			defer func() { _ = client.Close() }()
			return runCalibrationRecord(cmd.Context(), cmd.OutOrStdout(), client, record, asJSON)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&record.Alias, "alias", "", "binding alias this measurement is for (required)")
	flags.StringVar(&record.Provider, "provider", "", "provider type, e.g. claude or codex (required)")
	flags.StringVar(&record.InvocationHash, "invocation-hash", "",
		"hash identifying the exact command line measured (required)")
	flags.StringVar(&record.Model, "model", "", "concrete model measured")
	flags.StringVar(&record.Effort, "effort", "", "effort level measured")
	flags.StringVar(&record.BinaryPath, "binary-path", "", "path to the provider binary")
	flags.StringVar(&record.BinaryVersion, "binary-version", "", "provider binary version")
	flags.StringVar(&record.BinarySHA256, "binary-sha256", "", "provider binary digest")
	flags.StringVar(&record.InferenceDomain, "inference-domain", "", "who runs the inference")
	flags.StringVar(&record.ControlDomain, "control-domain", "", "who controls the endpoint")
	flags.StringVar(&record.IndependenceDomain, "independence-domain", "",
		"who a result from this provider is independent OF; what differentFrom compares")
	flags.StringVar(&record.ModelDigest, "model-digest", "", "model weights digest, when known")
	flags.BoolVar(&asJSON, "json", false, "emit the reply as JSON")
	return cmd
}

func runCalibrationRecord(
	ctx context.Context,
	out io.Writer,
	client calibrationClient,
	record ipc.CalibrationRecord,
	asJSON bool,
) error {
	reply, err := client.CalibrationPut(ctx, ipc.CalibrationPutArgs{Calibration: record})
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(out).Encode(reply)
	}
	// Report the domain back, or its absence. A calibration recorded without an
	// independence domain still satisfies the store, but it cannot support a
	// differentFrom constraint — and an operator who thinks they have just
	// enabled one deserves to be told they have not.
	if record.IndependenceDomain == "" {
		_, err = fmt.Fprintf(out,
			"recorded %s (no independence domain: differentFrom cannot be enforced against %s)\n",
			reply.ID, record.Alias)
		return err
	}
	_, err = fmt.Fprintf(out, "recorded %s (independence domain %q)\n",
		reply.ID, record.IndependenceDomain)
	return err
}

func newCalibrationListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded provider calibrations",
		Long: "List every recorded measurement, one per alias, so it is visible " +
			"which aliases have an independence domain and which do not.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := calibrationStateRoot()
			if err != nil {
				return err
			}
			client, err := supervisor.Find(root)
			if err != nil {
				return errNoSupervisorListening
			}
			defer func() { _ = client.Close() }()
			return runCalibrationList(cmd.Context(), cmd.OutOrStdout(), client, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the list as JSON")
	return cmd
}

func runCalibrationList(
	ctx context.Context,
	out io.Writer,
	client calibrationClient,
	asJSON bool,
) error {
	reply, err := client.CalibrationList(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(out).Encode(reply)
	}
	if len(reply.Calibrations) == 0 {
		_, err = fmt.Fprintln(out,
			"no calibrations recorded; every task records an empty independence "+
				"domain, so differentFrom cannot be enforced")
		return err
	}
	for _, c := range reply.Calibrations {
		domain := c.IndependenceDomain
		if domain == "" {
			domain = "(none — differentFrom cannot be enforced)"
		}
		if _, err := fmt.Fprintf(out, "%-20s %-10s %-16s %s\n",
			c.Alias, c.Provider, c.Model, domain); err != nil {
			return err
		}
	}
	return nil
}

// calibrationStateRoot resolves the supervisor state root.
//
// No project resolution, unlike the query commands: a calibration measures a
// PROVIDER on this host, not a project's work, so requiring a known project
// would refuse to record a measurement that is equally true everywhere.
func calibrationStateRoot() (string, error) {
	root, err := xdg.StateRoot()
	if err != nil {
		return "", fmt.Errorf("resolve state root: %w", err)
	}
	return root, nil
}
