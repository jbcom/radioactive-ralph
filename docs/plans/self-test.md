# Prove the build is sound

1. compile every target including the tagged GUI `accept: go build ./... && go build -tags gui ./internal/gui/`

   ```ralph-task
   {"id": "build"}
   ```

2. run the store's unit suite `accept: go test ./internal/store/`

   ```ralph-task
   {"id": "unit-store", "after": ["build"]}
   ```

3. run the observe + orchestrator suites `accept: go test ./internal/observe/ ./internal/orch/`

   ```ralph-task
   {"id": "unit-observe", "after": ["build"]}
   ```

4. run the race detector over the concurrency-bearing store `accept: go test -race ./internal/store/`

   ```ralph-task
   {"id": "race", "after": ["build"]}
   ```

5. lint the store package `accept: golangci-lint run ./internal/store/...`

   ```ralph-task
   {"id": "lint-store", "after": ["build"]}
   ```

Every step above carries an inline `accept:` command, so completion is
re-verified by the orchestrator rather than accepted on worker evidence. A step
without one is judgment-only, which is why the first dogfooding plan produced
nothing but failures.

**Keep each step's work small.** The first run of this plan had `unit` and
`lint` sweeping every package (`./internal/... ./cmd/...`), and both exhausted
their retry budget while `build` and `race` — one command, one package —
verified fine. A step whose provider turn outlives the turn deadline fails for
reasons that have nothing to do with the code, and reads identically to a real
failure. Split broad sweeps into per-package steps rather than raising the
deadline: narrower steps also tell you WHICH package broke.

# Prove the product actually runs

1. drive the real supervisor and TUI under a pty `accept: go test ./tests/e2e/ -count=1`

   ```ralph-task
   {"id": "e2e", "after": ["unit-store", "unit-observe", "race", "lint-store"]}
   ```

2. confirm the repo's own claim verifier agrees `accept: bash scripts/verify-repo-claims.sh`

   ```ralph-task
   {"id": "claims", "after": ["e2e"]}
   ```

"Tests pass" and "the app runs" are different claims; the e2e step is what
makes the second one checkable.
