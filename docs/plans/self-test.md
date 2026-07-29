# Prove the build is sound

1. compile every target including the tagged GUI `accept: go build ./... && go build -tags gui ./internal/gui/`

   ```ralph-task
   {"id": "build"}
   ```

2. run the unit suite `accept: go test ./internal/... ./cmd/...`

   ```ralph-task
   {"id": "unit", "after": ["build"]}
   ```

3. run the race detector over the concurrency-bearing store `accept: go test -race ./internal/store/`

   ```ralph-task
   {"id": "race", "after": ["build"]}
   ```

4. run the linter `accept: golangci-lint run ./internal/... ./cmd/...`

   ```ralph-task
   {"id": "lint", "after": ["build"]}
   ```

Every step above carries an inline `accept:` command, so completion is
re-verified by the orchestrator rather than accepted on worker evidence. A step
without one is judgment-only, which is why the first dogfooding plan produced
nothing but failures.

# Prove the product actually runs

1. drive the real supervisor and TUI under a pty `accept: go test ./tests/e2e/ -count=1`

   ```ralph-task
   {"id": "e2e", "after": ["unit", "race", "lint"]}
   ```

2. confirm the repo's own claim verifier agrees `accept: bash scripts/verify-repo-claims.sh`

   ```ralph-task
   {"id": "claims", "after": ["e2e"]}
   ```

"Tests pass" and "the app runs" are different claims; the e2e step is what
makes the second one checkable.
