# Prove the build is sound

1. compile every target, including the gui-tagged files in cmd `accept: go build ./... && go build -tags gui ./...`

   ```ralph-task
   {"id": "build"}
   ```

2. test the store `accept: go test ./internal/store/`

   ```ralph-task
   {"id": "unit-store", "after": ["build"]}
   ```

3. test the orchestration layer `accept: go test ./internal/orch/ ./internal/observe/ ./internal/plan/`

   ```ralph-task
   {"id": "unit-orch", "after": ["build"]}
   ```

4. test the provider and agent layer `accept: go test ./internal/provider/... ./internal/agent/ ./internal/agentdetect/ ./internal/contain/...`

   ```ralph-task
   {"id": "unit-provider", "after": ["build"]}
   ```

5. test the transport, service, and client layer `accept: go test ./internal/ipc/ ./internal/supervisor/ ./internal/service/ ./internal/tui/ ./internal/a2a/`

   ```ralph-task
   {"id": "unit-client", "after": ["build"]}
   ```

6. test the command and remaining support packages `accept: go test ./cmd/... ./internal/doctor/ ./internal/genesis/ ./internal/onboard/ ./internal/projectid/ ./internal/vconfig/ ./internal/xdg/ ./internal/rlog/ ./internal/statusbucket/`

   ```ralph-task
   {"id": "unit-cmd", "after": ["build"]}
   ```

7. test the integration and release-control suites `accept: go test ./tests/integration/ ./tests/releasecontrol/`

   ```ralph-task
   {"id": "unit-tests-dir", "after": ["build"]}
   ```

8. run the race detector over the concurrency-bearing store `accept: go test -race -v ./internal/store/`

   ```ralph-task
   {"id": "race", "after": ["build"]}
   ```

9. lint the internal packages `accept: golangci-lint run ./internal/...`

   ```ralph-task
   {"id": "lint-internal", "after": ["build"]}
   ```

10. lint the command packages `accept: golangci-lint run ./cmd/...`

    ```ralph-task
    {"id": "lint-cmd", "after": ["build"]}
    ```

Every step carries an inline `accept:` command, so completion is re-verified by
the orchestrator rather than accepted on worker evidence. A step without one is
judgment-only, which is why the first dogfooding plan produced nothing but
failures.

**A self-test run MUTATES the working tree, in two ways worth knowing before
you start one.**

It scatters scratch through the project dir on purpose -- a contained turn sets
HOME and TMPDIR under the containment root so its writes cannot escape, and
acceptance commands re-run in scratch trees of their own. Those are gitignored
(`.codex-*`, `.rr-accept.*`, `.tmp-*`), and they have to be: their contents
churn fast enough that `git add -A` does not merely stage junk, it FAILS
mid-stat on a file the turn already deleted.

More surprising: a step can EDIT TRACKED FILES. A provider turn trying to make
its acceptance command pass will change source to do it -- during one run the
`unit-client` step rewrote `internal/ipc/ipc_test.go`. That is the agent doing
its job, but it means a self-test running while you commit can put someone
else's edit in your staging area. Check `git status` before committing during a
run, and revert what you did not write.

**Coverage is maintained by hand; there is no test enforcing it.** I wrote one
and removed it after four attempts, each of which passed against a plan with a
package deliberately deleted: scanning the whole document let the LINT step's
`golangci-lint run ./internal/...` satisfy every internal package; scanning
lines containing "go test" then matched a PROSE paragraph explaining this very
rule; and the ancestor-glob matcher treated `./internal/...` as covering
everything. A guard that repeatedly fails to catch its own defect is worse than
a documented manual step, because it invites trust it has not earned. If you
add or move a package, add it to a step below and check `go list ./...`.

**Size steps to the turn deadline, but PARTITION coverage -- never drop it.**
A first revision split `go test ./internal/... ./cmd/...` down to the store
package alone, which fit the deadline and left a regression anywhere else able
to pass this self-test green. That is a check that verifies nothing. The unit
steps above cover every package in `go list ./...` between them; when a step
grows too slow, split it again rather than narrowing what is tested.

# Prove the product actually runs

1. drive the real supervisor and TUI under a pty `accept: go test ./tests/e2e/ -count=1`

   ```ralph-task
   {"id": "e2e", "after": ["unit-store", "unit-orch", "unit-provider", "unit-client", "unit-cmd", "unit-tests-dir", "race", "lint-internal", "lint-cmd"]}
   ```

2. confirm the repo's own claim verifier agrees `accept: bash scripts/verify-repo-claims.sh`

   ```ralph-task
   {"id": "claims", "after": ["e2e"]}
   ```

"Tests pass" and "the app runs" are different claims; the e2e step is what makes
the second one checkable.
