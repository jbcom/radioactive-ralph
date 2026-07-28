//go:build race

package provider

import "time"

// endlessOutputBudget under the RACE DETECTOR, which is why this constant is
// build-tagged rather than a single number.
//
// The detector instruments every memory access, and these tests deliberately
// push 16 MiB through a pty one 1 MiB line at a time. Measured on a 16-core
// macOS host, the SAME runner path costs 2.06s without -race and 39.73s with
// it -- a ~19x multiplier, with the ceiling error CORRECT in both cases. The
// full provider package under -race takes ~186s idle.
//
// 300s is ~7.5x the measured 40s cost, which is headroom for a loaded CI runner
// without being a number nobody can justify. It is bounded ABOVE by the package
// clock, and that bound is the real constraint: `go test` applies its timeout to
// the whole BINARY, three endless-output tests run serially, so a per-test
// budget larger than (package timeout - rest of suite) / 3 can never actually be
// spent. The binary panics first, and the failure resurfaces as an
// infrastructure error instead of a test one.
//
//	3 x 300s = 900s of endless-output tests
//	+ ~190s for the rest of the package
//	= ~1090s, against the explicit -timeout 20m (1200s) CI now passes.
//
// CI passes -timeout explicitly (.github/workflows/ci.yml) rather than relying
// on the 10-minute default, which the aggregate above would exceed. Stating it
// there keeps the ceiling visible instead of implicit.
//
// A budget cannot be made large enough to survive ARBITRARY contention -- under
// ~12 spinners/core, far heavier than any CI runner, even 150s deadlined. That
// is not what this defends against. It defends against the ordinary -race
// slowdown, which is deterministic and measured.
const endlessOutputBudget = 300 * time.Second
