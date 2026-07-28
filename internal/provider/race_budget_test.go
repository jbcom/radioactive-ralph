//go:build !race

package provider

import "time"

// endlessOutputBudget bounds a test that must read its way to the cumulative
// output ceiling before asserting on the resulting error.
//
// Measured on a 16-core macOS host with the 16 MiB ceiling and a 1 MiB-per-line
// firehose: the ceiling itself trips in ~370ms and the full runner path returns
// ErrObservedOutputTooLarge in ~2s. The budget clears that by a wide margin so a
// slow CI runner does not turn a passing assertion into a deadline.
const endlessOutputBudget = 90 * time.Second
