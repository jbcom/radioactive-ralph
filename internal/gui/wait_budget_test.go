package gui

import "time"

// guiWaitBudget is the deadline for every POSITIVE wait in this package --
// "did the window open", "did Run return", "did the first frame paint".
//
// It is deliberately far larger than the work these waits observe, because the
// assertion is "does this EVER happen", not "does it happen within N". No
// product requirement says the first frame lands in 3s, and a headless CI
// runner is slower and noisier than any dev machine.
//
// The former 3s value failed TestRun_StartsAndStopsCleanly on ubuntu-latest
// while passing 3/3 locally -- the tell that a threshold is measuring runner
// speed rather than correctness. A hang still fails here, just later, and a
// test that needs 30s to report a real hang beats one that reports a phantom
// hang on a busy runner.
//
// NOT used for negative assertions ("this must NOT complete yet"), where a
// short timeout IS the assertion; those keep their own deliberately-brief
// budgets.
//
// THIS FILE CARRIES NO BUILD TAG, deliberately. The constant first lived in
// app_test.go, which is `//go:build gui` -- but live_test.go is untagged, so
// the untagged build CI actually runs could not see it and failed to compile.
// Verifying only with `-tags gui` is how that shipped: the tagged build was
// the one configuration I checked, and it was the one that already worked.
const guiWaitBudget = 30 * time.Second
