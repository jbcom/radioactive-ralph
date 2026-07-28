//go:build race

package provider

import "time"

// endlessOutputBudget under the RACE DETECTOR, which is the whole reason this
// constant is build-tagged rather than a single number.
//
// The detector instruments every memory access, and these tests deliberately
// push 16 MiB through a pty one 1 MiB line at a time. Measured on the same
// host: 2.06s without -race, 39.73s with it -- a ~19x multiplier, with the
// ceiling error CORRECT in both cases. A 90s budget therefore left ~50s of
// headroom, and under CPU contention (~12 spinners/core, reproduced) the run
// crossed it and failed at exactly 90.01s with "context deadline exceeded,
// want ErrObservedOutputTooLarge".
//
// That failure read like a product bug -- the ceiling not tripping -- and was
// filed as one (#273). It is not: the ceiling works, the budget was too tight
// for the instrumented case. Scaling the budget with the build keeps the test
// asserting on BEHAVIOR rather than on how fast the host happens to be.
const endlessOutputBudget = 600 * time.Second
