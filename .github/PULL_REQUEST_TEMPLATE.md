## Summary

<!-- What does this PR change and why? -->

## Verification

<!-- Check the items that apply. The repo's testing doctrine (AGENTS.md) has more detail. -->

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `go test -race ./...` passes (for concurrency-touching packages)
- [ ] `golangci-lint run` passes (0 issues)
- [ ] If this PR adds a new task field, all three renderers are covered (TUI, GUI, CLI `status`)
- [ ] If this PR adds a new check, it FAILS on empty input (not silently passes)
- [ ] If this PR fixes a bug, the fix was reverted and the named test failed for the stated reason
- [ ] Docs and tests stay aligned with code (no end-of-project catch-up)

## Acceptance markers

<!-- If this PR implements a plan step with an inline `accept:` marker, note it here. -->