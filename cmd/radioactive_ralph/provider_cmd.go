package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

func newProviderCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "provider", Short: "Manage evidence-backed provider bindings"}
	calibration := &cobra.Command{
		Use: "calibration", Short: "Import and inspect immutable capability calibrations",
	}
	calibration.AddCommand(newCalibrationImportCmd(), newCalibrationShowCmd(), newCalibrationListCmd())
	cmd.AddCommand(calibration)
	return cmd
}

func newCalibrationImportCmd() *cobra.Command {
	return &cobra.Command{
		Use: "import <calibration.json>", Short: "Validate and import a calibration record",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			record, err := readCalibrationRecord(args[0])
			if err != nil {
				return err
			}
			id, err := putCalibration(cmd.Context(), record)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", id)
			return err
		},
	}
}

func newCalibrationShowCmd() *cobra.Command {
	return &cobra.Command{
		Use: "show <sha256:id>", Short: "Print one calibration as JSON",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			records, err := getCalibrations(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), records[0])
		},
	}
}

func newCalibrationListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "ls", Aliases: []string{"list"}, Short: "List calibrated binding aliases",
		RunE: func(cmd *cobra.Command, _ []string) error {
			records, err := getCalibrations(cmd.Context(), "")
			if err != nil {
				return err
			}
			for _, record := range records {
				if _, err := fmt.Fprintf(
					cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n",
					record.ID, record.Alias, record.Provider, record.Model, record.Effort,
				); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func readCalibrationRecord(path string) (ipc.CalibrationRecord, error) {
	file, err := os.Open(path) //nolint:gosec // operator-supplied calibration path
	if err != nil {
		return ipc.CalibrationRecord{}, fmt.Errorf("open calibration: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var record ipc.CalibrationRecord
	if err := decoder.Decode(&record); err != nil {
		return ipc.CalibrationRecord{}, fmt.Errorf("decode calibration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ipc.CalibrationRecord{}, fmt.Errorf("decode calibration: trailing JSON value")
	}
	return record, nil
}

func putCalibration(ctx context.Context, record ipc.CalibrationRecord) (string, error) {
	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return "", err
	}
	if client, err := supervisor.Find(stateRoot); err == nil {
		defer func() { _ = client.Close() }()
		reply, err := client.CalibrationPut(ctx, ipc.CalibrationPutArgs{Calibration: record})
		return reply.ID, err
	}
	value := calibrationRecordToStore(record)
	if _, err := orch.ValidateProviderCalibration(value); err != nil {
		return "", fmt.Errorf("validate calibration: %w", err)
	}
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return "", err
	}
	defer func() { _ = st.Close() }()
	return st.PutProviderCalibration(ctx, value)
}

func getCalibrations(ctx context.Context, id string) ([]ipc.CalibrationRecord, error) {
	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return nil, err
	}
	if client, err := supervisor.Find(stateRoot); err == nil {
		defer func() { _ = client.Close() }()
		if id != "" {
			record, err := client.CalibrationGet(ctx, ipc.CalibrationGetArgs{ID: id})
			return []ipc.CalibrationRecord{record}, err
		}
		reply, err := client.CalibrationList(ctx)
		return reply.Calibrations, err
	}
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return nil, err
	}
	defer func() { _ = st.Close() }()
	if id != "" {
		value, err := st.GetProviderCalibration(ctx, id)
		return []ipc.CalibrationRecord{calibrationStoreToRecord(value)}, err
	}
	values, err := st.ListProviderCalibrations(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]ipc.CalibrationRecord, 0, len(values))
	for _, value := range values {
		records = append(records, calibrationStoreToRecord(value))
	}
	return records, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
